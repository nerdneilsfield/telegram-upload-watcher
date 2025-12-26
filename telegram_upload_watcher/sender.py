from __future__ import annotations

import asyncio
import logging
from dataclasses import dataclass
from pathlib import Path
import zipfile

from .image_processing import prepare_image_bytes
from .queue import JsonlQueue, QueueItem, STATUS_FAILED, STATUS_SENDING, STATUS_SENT
from .telegram import send_media_group
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


def _load_item_bytes(item: QueueItem) -> tuple[bytes, str]:
    if item.source_type == "file":
        path = Path(item.path)
        return path.read_bytes(), path.name

    if item.source_type == "zip":
        zip_path = Path(item.path)
        with zipfile.ZipFile(zip_path, "r") as zip_ref:
            with zip_ref.open(item.inner_path or "") as handle:
                data = handle.read()
        filename = Path(item.inner_path or "image").name
        return data, filename

    raise ValueError(f"Unsupported source_type: {item.source_type}")


async def _send_group(
    config: SenderConfig,
    queue: JsonlQueue,
    url_pool: UrlPool,
    token_pool: TokenPool,
    items: list[QueueItem],
) -> int:
    media_files: list[tuple[str, bytes]] = []
    item_refs: list[QueueItem] = []

    for item in items:
        queue.update_status(item.id, STATUS_SENDING)
        try:
            data, filename = _load_item_bytes(item)
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
            queue.update_status(item.id, STATUS_FAILED, error=str(exc))

    if not media_files:
        return 0

    try:
        await send_media_group(
            url_pool,
            token_pool,
            config.chat_id,
            media_files,
            topic_id=config.topic_id,
        )
    except Exception as exc:
        logging.warning("send_media_group failed: %s", exc)
        for item in item_refs:
            queue.update_status(item.id, STATUS_FAILED, error=str(exc))
        return 0

    for item in item_refs:
        queue.update_status(item.id, STATUS_SENT)
    return len(item_refs)


async def sender_loop(
    config: SenderConfig,
    queue: JsonlQueue,
    url_pool: UrlPool,
    token_pool: TokenPool,
) -> None:
    sent_since_pause = 0
    while True:
        pending = queue.get_pending()
        if not pending:
            await asyncio.sleep(config.send_interval)
            continue

        for idx in range(0, len(pending), config.group_size):
            group = pending[idx : idx + config.group_size]
            sent_count = await _send_group(config, queue, url_pool, token_pool, group)
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
