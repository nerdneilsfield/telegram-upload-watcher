from __future__ import annotations

import asyncio
import logging
from dataclasses import dataclass
from pathlib import Path

from .image_processing import prepare_image_bytes
from .telegram import open_zip_entry
from .queue import JsonlQueue, QueueItem, STATUS_FAILED, STATUS_SENDING, STATUS_SENT
from .telegram import send_audio, send_document, send_media_group, send_video
from .pools import TokenPool, UrlPool


@dataclass
class SenderConfig:
    chat_id: str
    topic_id: int | None
    group_size: int
    send_interval: int
    batch_delay: int
    pause_every: int
    pause_seconds: int
    max_dimension: int
    max_bytes: int
    png_start_level: int
    max_retries: int
    retry_delay: int


def _load_item_bytes(item: QueueItem, zip_passwords: list[str]) -> tuple[bytes, str]:
    if item.source_type == "file":
        path = Path(item.path)
        return path.read_bytes(), path.name

    if item.source_type == "zip":
        zip_path = Path(item.path)
        data, name = open_zip_entry(zip_path, item.inner_path, zip_passwords)
        return data, name

    raise ValueError(f"Unsupported source_type: {item.source_type}")


def _mark_failed(queue: JsonlQueue, item: QueueItem, exc: Exception) -> None:
    attempts = item.attempts + 1
    queue.update_status(item.id, STATUS_FAILED, error=str(exc), attempts=attempts)


async def _send_image_group(
    config: SenderConfig,
    queue: JsonlQueue,
    url_pool: UrlPool,
    token_pool: TokenPool,
    items: list[QueueItem],
    zip_passwords: list[str],
) -> int:
    media_files: list[tuple[str, bytes]] = []
    item_refs: list[QueueItem] = []

    for item in items:
        queue.update_status(item.id, STATUS_SENDING)
        try:
            data, filename = _load_item_bytes(item, zip_passwords)
            processed, send_name = prepare_image_bytes(
                data,
                filename,
                max_dimension=config.max_dimension,
                max_bytes=config.max_bytes,
                png_start_level=config.png_start_level,
            )
            media_files.append((send_name, processed))
            item_refs.append(item)
        except Exception as exc:
            logging.warning("Failed to prepare %s: %s", item.path, exc)
            _mark_failed(queue, item, exc)

    if not media_files:
        return 0

    try:
        await send_media_group(
            url_pool,
            token_pool,
            config.chat_id,
            media_files,
            topic_id=config.topic_id,
            max_retries=config.max_retries,
            retry_delay=config.retry_delay,
        )
    except Exception as exc:
        logging.warning("send_media_group failed: %s", exc)
        for item in item_refs:
            _mark_failed(queue, item, exc)
        return 0

    for item in item_refs:
        queue.update_status(item.id, STATUS_SENT)
    return len(item_refs)


async def _send_single(
    config: SenderConfig,
    queue: JsonlQueue,
    url_pool: UrlPool,
    token_pool: TokenPool,
    item: QueueItem,
    send_type: str,
    zip_passwords: list[str],
) -> int:
    queue.update_status(item.id, STATUS_SENDING)
    try:
        data, filename = _load_item_bytes(item, zip_passwords)
        if send_type == "file":
            await send_document(
                url_pool,
                token_pool,
                config.chat_id,
                filename,
                data,
                topic_id=config.topic_id,
                max_retries=config.max_retries,
                retry_delay=config.retry_delay,
            )
        elif send_type == "video":
            await send_video(
                url_pool,
                token_pool,
                config.chat_id,
                filename,
                data,
                topic_id=config.topic_id,
                max_retries=config.max_retries,
                retry_delay=config.retry_delay,
            )
        elif send_type == "audio":
            await send_audio(
                url_pool,
                token_pool,
                config.chat_id,
                filename,
                data,
                topic_id=config.topic_id,
                max_retries=config.max_retries,
                retry_delay=config.retry_delay,
            )
        else:
            raise ValueError(f"Unsupported send_type: {send_type}")
    except Exception as exc:
        logging.warning("Failed to send %s: %s", item.path, exc)
        _mark_failed(queue, item, exc)
        return 0

    queue.update_status(item.id, STATUS_SENT)
    return 1


async def sender_loop(
    config: SenderConfig,
    queue: JsonlQueue,
    url_pool: UrlPool,
    token_pool: TokenPool,
    zip_passwords: list[str] | None = None,
) -> None:
    zip_passwords = zip_passwords or []
    sent_since_pause = 0
    while True:
        pending = queue.get_pending()
        if not pending:
            await asyncio.sleep(config.send_interval)
            continue

        idx = 0
        while idx < len(pending):
            item = pending[idx]
            send_type = item.send_type or "image"
            if send_type == "image":
                group: list[QueueItem] = []
                while idx < len(pending) and len(group) < config.group_size:
                    current = pending[idx]
                    current_type = current.send_type or "image"
                    if current_type != "image":
                        break
                    group.append(current)
                    idx += 1
                sent_count = await _send_image_group(
                    config, queue, url_pool, token_pool, group, zip_passwords
                )
            else:
                sent_count = await _send_single(
                    config,
                    queue,
                    url_pool,
                    token_pool,
                    item,
                    send_type,
                    zip_passwords,
                )
                idx += 1
            sent_since_pause += sent_count
            await asyncio.sleep(config.batch_delay)

            if (
                config.pause_every > 0
                and sent_since_pause >= config.pause_every
                and config.pause_seconds > 0
            ):
                logging.info(
                    "Pausing sender for %d seconds after %d images",
                    config.pause_seconds,
                    sent_since_pause,
                )
                await asyncio.sleep(config.pause_seconds)
                sent_since_pause = 0

        await asyncio.sleep(config.send_interval)


async def drain_queue(
    config: SenderConfig,
    queue: JsonlQueue,
    url_pool: UrlPool,
    token_pool: TokenPool,
    *,
    zip_passwords: list[str] | None = None,
    queue_retries: int = 3,
    progress: bool = True,
) -> tuple[int, int, int]:
    zip_passwords = zip_passwords or []
    max_attempts = queue_retries if queue_retries > 0 else None
    pending = queue.get_pending(max_attempts=max_attempts)
    if not pending:
        return 0, 0, 0

    progress_bar = None
    if progress:
        try:
            import tqdm

            progress_bar = tqdm.tqdm(total=len(pending))
        except Exception:
            progress_bar = None

    def advance(count: int) -> None:
        if progress_bar is not None:
            progress_bar.update(count)

    sent = 0
    skipped = 0
    sent_bytes = 0
    idx = 0
    while idx < len(pending):
        item = pending[idx]
        send_type = item.send_type or "image"
        if send_type == "image":
            group: list[QueueItem] = []
            while idx < len(pending) and len(group) < config.group_size:
                current = pending[idx]
                current_type = current.send_type or "image"
                if current_type != "image":
                    break
                group.append(current)
                idx += 1

            media_files: list[tuple[str, bytes]] = []
            item_refs: list[QueueItem] = []
            group_bytes = 0
            for entry in group:
                queue.update_status(entry.id, STATUS_SENDING)
                try:
                    data, filename = _load_item_bytes(entry, zip_passwords)
                    processed, send_name = prepare_image_bytes(
                        data,
                        filename,
                        max_dimension=config.max_dimension,
                        max_bytes=config.max_bytes,
                        png_start_level=config.png_start_level,
                    )
                    media_files.append((send_name, processed))
                    item_refs.append(entry)
                    group_bytes += len(data)
                except Exception as exc:
                    logging.warning("Failed to prepare %s: %s", entry.path, exc)
                    _mark_failed(queue, entry, exc)
                    skipped += 1

            if media_files:
                try:
                    await send_media_group(
                        url_pool,
                        token_pool,
                        config.chat_id,
                        media_files,
                        topic_id=config.topic_id,
                        max_retries=config.max_retries,
                        retry_delay=config.retry_delay,
                    )
                except Exception as exc:
                    logging.warning("send_media_group failed: %s", exc)
                    for entry in item_refs:
                        _mark_failed(queue, entry, exc)
                    skipped += len(item_refs)
                else:
                    for entry in item_refs:
                        queue.update_status(entry.id, STATUS_SENT)
                    sent += len(item_refs)
                    sent_bytes += group_bytes
                await asyncio.sleep(config.batch_delay)

            advance(len(group))
            continue

        queue.update_status(item.id, STATUS_SENDING)
        try:
            data, filename = _load_item_bytes(item, zip_passwords)
            if send_type == "file":
                await send_document(
                    url_pool,
                    token_pool,
                    config.chat_id,
                    filename,
                    data,
                    topic_id=config.topic_id,
                    max_retries=config.max_retries,
                    retry_delay=config.retry_delay,
                )
            elif send_type == "video":
                await send_video(
                    url_pool,
                    token_pool,
                    config.chat_id,
                    filename,
                    data,
                    topic_id=config.topic_id,
                    max_retries=config.max_retries,
                    retry_delay=config.retry_delay,
                )
            elif send_type == "audio":
                await send_audio(
                    url_pool,
                    token_pool,
                    config.chat_id,
                    filename,
                    data,
                    topic_id=config.topic_id,
                    max_retries=config.max_retries,
                    retry_delay=config.retry_delay,
                )
            else:
                raise ValueError(f"Unsupported send_type: {send_type}")
        except Exception as exc:
            logging.warning("Failed to send %s: %s", item.path, exc)
            _mark_failed(queue, item, exc)
            skipped += 1
        else:
            queue.update_status(item.id, STATUS_SENT)
            sent += 1
            sent_bytes += len(data)

        advance(1)
        idx += 1
        await asyncio.sleep(config.batch_delay)

    if progress_bar is not None:
        progress_bar.close()

    return sent, skipped, sent_bytes
