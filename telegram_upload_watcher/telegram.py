import asyncio
import fnmatch
import json
import logging
import mimetypes
import os
import time
import zipfile
from datetime import datetime
from pathlib import Path

import aiohttp
import requests
import tqdm

from .constants import AUDIO_EXTENSIONS, IMAGE_EXTENSIONS, VIDEO_EXTENSIONS
from .net import get_proxy_from_env
from .pools import TokenPool, UrlPool


def test_token(api_url: str, bot_token: str) -> bool:
    url = f"{api_url}/bot{bot_token}/getMe"
    proxy = get_proxy_from_env()
    try:
        response = requests.get(
            url, proxies={"https": proxy} if proxy else None, timeout=15
        )
        data = response.json()
        logging.info("Token test response: %s", data)
        return bool(data.get("ok"))
    except Exception as exc:
        logging.error("Token test failed: %s", exc)
        return False


async def send_message(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    text: str,
    *,
    topic_id: int | None = None,
    max_retries: int = 3,
    retry_delay: int = 3,
) -> bool:
    for attempt in range(1, max_retries + 1):
        try:
            return await _send_message_once(
                url_pool, token_pool, chat_id, text, topic_id=topic_id
            )
        except Exception as exc:
            if attempt >= max_retries:
                raise
            logging.warning(
                "send_message failed (attempt %d/%d): %s",
                attempt,
                max_retries,
                exc,
            )
            await asyncio.sleep(retry_delay)
    return False


def _guess_content_type(filename: str) -> str:
    mime_type, _ = mimetypes.guess_type(filename)
    return mime_type or "application/octet-stream"


def _format_timestamp(value: datetime | None = None) -> str:
    if value is None:
        value = datetime.now()
    return value.strftime("%Y-%m-%d %H:%M:%S")


def _format_duration(seconds: float) -> str:
    if seconds <= 0:
        return "0s"
    if seconds < 1:
        return f"{int(seconds * 1000)}ms"
    total = int(round(seconds))
    hours, remainder = divmod(total, 3600)
    minutes, secs = divmod(remainder, 60)
    if hours:
        return f"{hours}h{minutes}m{secs}s"
    if minutes:
        return f"{minutes}m{secs}s"
    return f"{secs}s"


def _format_bytes(size: int) -> str:
    if size < 1024:
        return f"{size} B"
    units = ["KB", "MB", "GB", "TB", "PB"]
    value = float(size)
    idx = 0
    while value >= 1024 and idx < len(units) - 1:
        value /= 1024
        idx += 1
    return f"{value:.1f} {units[idx]}"


def _format_speed(total_bytes: int, elapsed_seconds: float) -> str:
    if total_bytes <= 0 or elapsed_seconds <= 0:
        return "0 B/s"
    per_second = total_bytes / elapsed_seconds
    return f"{_format_bytes(int(per_second))}/s"


def _print_summary(
    kind: str,
    source: str,
    started_at: datetime,
    finished_at: datetime,
    elapsed_seconds: float,
    sent: int,
    skipped: int,
    total_bytes: int,
) -> None:
    avg_per = elapsed_seconds / sent if sent > 0 else 0.0
    start_value = _format_timestamp(started_at)
    end_value = _format_timestamp(finished_at)
    elapsed_value = _format_duration(elapsed_seconds)
    avg_value = _format_duration(avg_per)
    total_value = _format_bytes(total_bytes)
    speed_value = _format_speed(total_bytes, elapsed_seconds)

    try:
        from rich import box
        from rich.console import Console
        from rich.table import Table
    except Exception:
        print(
            "Summary %s from %s: start=%s end=%s elapsed=%s avg=%s total=%s speed=%s sent=%d skipped=%d"
            % (
                kind,
                source,
                start_value,
                end_value,
                elapsed_value,
                avg_value,
                total_value,
                speed_value,
                sent,
                skipped,
            )
        )
        return

    table = Table(title="Summary", box=box.ASCII)
    table.add_column("Metric", style="bold")
    table.add_column("Value")
    table.add_row("Kind", kind)
    table.add_row("Source", source)
    table.add_row("Start", start_value)
    table.add_row("End", end_value)
    table.add_row("Elapsed", elapsed_value)
    table.add_row("Avg per item", avg_value)
    table.add_row("Total", total_value)
    table.add_row("Avg speed", speed_value)
    table.add_row("Sent", str(sent))
    table.add_row("Skipped", str(skipped))

    Console().print(table)


async def _send_message_once(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    text: str,
    *,
    topic_id: int | None = None,
) -> bool:
    api_url = url_pool.get_url()
    bot_token = token_pool.get_token()
    if not api_url or not bot_token:
        raise RuntimeError("No available API URL or token")

    url = f"{api_url}/bot{bot_token}/sendMessage"
    form_data = aiohttp.FormData()
    form_data.add_field("chat_id", str(chat_id))
    form_data.add_field("text", text)
    if topic_id:
        form_data.add_field("message_thread_id", str(topic_id))

    proxy = get_proxy_from_env()
    try:
        async with aiohttp.ClientSession() as session:
            async with session.post(
                url, data=form_data, proxy=proxy, ssl=False if proxy else None
            ) as response:
                data = await response.json()
                if data.get("ok"):
                    token_pool.increment_token(bot_token)
                    return True
                error_msg = data.get("description", "Unknown error")
                if "Too Many Requests" in error_msg:
                    retry_after = data.get("parameters", {}).get("retry_after")
                    if retry_after:
                        await asyncio.sleep(retry_after)
                else:
                    token_pool.remove_token(bot_token)
                raise RuntimeError(f"Telegram sendMessage failed: {error_msg}")
    finally:
        url_pool.increment_url(api_url)


async def send_media_group(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    media_files: list[tuple[str, bytes]],
    *,
    topic_id: int | None = None,
    max_retries: int = 3,
    retry_delay: int = 3,
) -> bool:
    for attempt in range(1, max_retries + 1):
        try:
            return await _send_media_group_once(
                url_pool, token_pool, chat_id, media_files, topic_id=topic_id
            )
        except Exception as exc:
            if attempt >= max_retries:
                raise
            logging.warning(
                "send_media_group failed (attempt %d/%d): %s",
                attempt,
                max_retries,
                exc,
            )
            await asyncio.sleep(retry_delay)
    return False


async def _send_media_group_once(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    media_files: list[tuple[str, bytes]],
    *,
    topic_id: int | None = None,
) -> bool:
    api_url = url_pool.get_url()
    bot_token = token_pool.get_token()
    if not api_url or not bot_token:
        raise RuntimeError("No available API URL or token")

    url = f"{api_url}/bot{bot_token}/sendMediaGroup"
    form_data = aiohttp.FormData()
    form_data.add_field("chat_id", str(chat_id))
    if topic_id:
        form_data.add_field("message_thread_id", str(topic_id))

    media_list = []
    for index, (filename, image_data) in enumerate(media_files):
        file_key = f"file{index}"
        media_list.append({"type": "photo", "media": f"attach://{file_key}"})
        form_data.add_field(
            file_key,
            image_data,
            filename=filename,
            content_type=_guess_content_type(filename),
        )
    form_data.add_field("media", json.dumps(media_list))

    proxy = get_proxy_from_env()
    try:
        async with aiohttp.ClientSession() as session:
            async with session.post(
                url, data=form_data, proxy=proxy, ssl=False if proxy else None
            ) as response:
                data = await response.json()
                if data.get("ok"):
                    token_pool.increment_token(bot_token)
                    return True
                error_msg = data.get("description", "Unknown error")
                if "Too Many Requests" in error_msg:
                    retry_after = data.get("parameters", {}).get("retry_after")
                    if retry_after:
                        await asyncio.sleep(retry_after)
                else:
                    token_pool.remove_token(bot_token)
                raise RuntimeError(f"Telegram sendMediaGroup failed: {error_msg}")
    finally:
        url_pool.increment_url(api_url)


async def send_document(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    filename: str,
    data: bytes,
    *,
    topic_id: int | None = None,
    max_retries: int = 3,
    retry_delay: int = 3,
) -> bool:
    return await _send_file_with_retry(
        url_pool,
        token_pool,
        chat_id,
        "sendDocument",
        "document",
        filename,
        data,
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
    )


async def send_video(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    filename: str,
    data: bytes,
    *,
    topic_id: int | None = None,
    max_retries: int = 3,
    retry_delay: int = 3,
) -> bool:
    return await _send_file_with_retry(
        url_pool,
        token_pool,
        chat_id,
        "sendVideo",
        "video",
        filename,
        data,
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
    )


async def send_audio(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    filename: str,
    data: bytes,
    *,
    topic_id: int | None = None,
    max_retries: int = 3,
    retry_delay: int = 3,
) -> bool:
    return await _send_file_with_retry(
        url_pool,
        token_pool,
        chat_id,
        "sendAudio",
        "audio",
        filename,
        data,
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
    )


async def _send_file_with_retry(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    endpoint: str,
    field_name: str,
    filename: str,
    data: bytes,
    *,
    topic_id: int | None = None,
    max_retries: int = 3,
    retry_delay: int = 3,
) -> bool:
    for attempt in range(1, max_retries + 1):
        try:
            return await _send_file_once(
                url_pool,
                token_pool,
                chat_id,
                endpoint,
                field_name,
                filename,
                data,
                topic_id=topic_id,
            )
        except Exception as exc:
            if attempt >= max_retries:
                raise
            logging.warning(
                "%s failed (attempt %d/%d): %s",
                endpoint,
                attempt,
                max_retries,
                exc,
            )
            await asyncio.sleep(retry_delay)
    return False


async def _send_file_once(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    endpoint: str,
    field_name: str,
    filename: str,
    data: bytes,
    *,
    topic_id: int | None = None,
) -> bool:
    api_url = url_pool.get_url()
    bot_token = token_pool.get_token()
    if not api_url or not bot_token:
        raise RuntimeError("No available API URL or token")

    url = f"{api_url}/bot{bot_token}/{endpoint}"
    form_data = aiohttp.FormData()
    form_data.add_field("chat_id", str(chat_id))
    if topic_id:
        form_data.add_field("message_thread_id", str(topic_id))
    form_data.add_field(
        field_name,
        data,
        filename=filename,
        content_type=_guess_content_type(filename),
    )

    proxy = get_proxy_from_env()
    try:
        async with aiohttp.ClientSession() as session:
            async with session.post(
                url, data=form_data, proxy=proxy, ssl=False if proxy else None
            ) as response:
                data = await response.json()
                if data.get("ok"):
                    token_pool.increment_token(bot_token)
                    return True
                error_msg = data.get("description", "Unknown error")
                if "Too Many Requests" in error_msg:
                    retry_after = data.get("parameters", {}).get("retry_after")
                    if retry_after:
                        await asyncio.sleep(retry_after)
                else:
                    token_pool.remove_token(bot_token)
                raise RuntimeError(f"Telegram {endpoint} failed: {error_msg}")
    finally:
        url_pool.increment_url(api_url)


def _matches_include(rel_path: str, patterns: list[str]) -> bool:
    if not patterns:
        return True
    for pattern in patterns:
        if not pattern:
            continue
        if fnmatch.fnmatch(rel_path, pattern):
            return True
    return False


def _matches_exclude(rel_path: str, patterns: list[str]) -> bool:
    for pattern in patterns:
        if not pattern:
            continue
        if fnmatch.fnmatch(rel_path, pattern):
            return True
    return False


def _mixed_send_type(
    name: str,
    *,
    with_image: bool,
    with_video: bool,
    with_audio: bool,
    with_file: bool,
) -> str | None:
    lower = name.lower()
    if with_image and lower.endswith(IMAGE_EXTENSIONS):
        return "image"
    if with_video and lower.endswith(VIDEO_EXTENSIONS):
        return "video"
    if with_audio and lower.endswith(AUDIO_EXTENSIONS):
        return "audio"
    if with_file:
        return "file"
    return None


def _collect_image_files(
    image_dir: Path,
    include_globs: list[str],
    exclude_globs: list[str],
    enable_zip: bool,
) -> list[Path]:
    files: list[Path] = []
    for root, _, filenames in os.walk(image_dir):
        for filename in filenames:
            rel_path = str(Path(root).relative_to(image_dir) / filename)
            if not _matches_include(rel_path, include_globs):
                continue
            if _matches_exclude(rel_path, exclude_globs):
                continue
            lower_name = filename.lower()
            is_image = lower_name.endswith(IMAGE_EXTENSIONS)
            is_zip = lower_name.endswith(".zip")
            if not is_image and not (enable_zip and is_zip):
                continue
            path = Path(root) / filename
            if path.is_file():
                files.append(path)
    return files


def _collect_files(
    root_dir: Path,
    include_globs: list[str],
    exclude_globs: list[str],
    enable_zip: bool,
    allowed_exts: tuple[str, ...] | None,
) -> list[Path]:
    files: list[Path] = []
    for root, _, filenames in os.walk(root_dir):
        for filename in filenames:
            rel_path = str(Path(root).relative_to(root_dir) / filename)
            if not _matches_include(rel_path, include_globs):
                continue
            if _matches_exclude(rel_path, exclude_globs):
                continue
            lower_name = filename.lower()
            is_zip = lower_name.endswith(".zip")
            if is_zip:
                if enable_zip or allowed_exts is None:
                    path = Path(root) / filename
                    if path.is_file():
                        files.append(path)
                continue
            if allowed_exts is None or lower_name.endswith(allowed_exts):
                path = Path(root) / filename
                if path.is_file():
                    files.append(path)
    return files


def _collect_source_files(
    root_dir: Path,
    include_globs: list[str],
    exclude_globs: list[str],
) -> list[Path]:
    files: list[Path] = []
    for root, _, filenames in os.walk(root_dir):
        for filename in filenames:
            rel_path = str(Path(root).relative_to(root_dir) / filename)
            if not _matches_include(rel_path, include_globs):
                continue
            if _matches_exclude(rel_path, exclude_globs):
                continue
            path = Path(root) / filename
            if path.is_file():
                files.append(path)
    return files


async def send_images_from_dir(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    image_dir: Path,
    *,
    topic_id: int | None = None,
    group_size: int = 4,
    start_index: int = 0,
    end_index: int = 0,
    batch_delay: int = 3,
    progress: bool = True,
    include_globs: list[str] | None = None,
    exclude_globs: list[str] | None = None,
    enable_zip: bool = False,
    zip_passwords: list[str] | None = None,
    max_retries: int = 3,
    retry_delay: int = 3,
) -> None:
    include_globs = include_globs or []
    exclude_globs = exclude_globs or []
    zip_passwords = zip_passwords or []
    files = _collect_image_files(image_dir, include_globs, exclude_globs, enable_zip)
    if not files:
        logging.info("No images found in %s", image_dir)
        return

    started_at = datetime.now()
    started_clock = time.monotonic()
    sent = 0
    skipped = 0
    sent_bytes = 0
    await send_message(
        url_pool,
        token_pool,
        chat_id,
        f"Starting image upload: {len(files)} file(s) at {_format_timestamp(started_at)}",
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
    )

    min_index = start_index * group_size
    max_index = end_index * group_size if end_index else None
    media_files: list[tuple[str, bytes]] = []
    batch_bytes = 0
    iterator = tqdm.tqdm(files) if progress else files
    for idx, path in enumerate(iterator):
        if idx < min_index:
            continue
        if max_index is not None and idx >= max_index:
            break

        if path.name.lower().endswith(".zip"):
            await send_images_from_zip(
                url_pool,
                token_pool,
                chat_id,
                path,
                topic_id=topic_id,
                group_size=group_size,
                start_index=0,
                end_index=0,
                batch_delay=batch_delay,
                progress=progress,
                include_globs=include_globs,
                exclude_globs=exclude_globs,
                zip_passwords=zip_passwords,
                max_retries=max_retries,
                retry_delay=retry_delay,
            )
            continue
        try:
            with path.open("rb") as image_file:
                data = image_file.read()
        except OSError as exc:
            logging.warning("Failed to read %s: %s", path, exc)
            skipped += 1
            continue
        media_files.append((path.name, data))
        batch_bytes += len(data)
        if len(media_files) >= group_size:
            await send_media_group(
                url_pool,
                token_pool,
                chat_id,
                media_files,
                topic_id=topic_id,
                max_retries=max_retries,
                retry_delay=retry_delay,
            )
            sent += len(media_files)
            sent_bytes += batch_bytes
            media_files = []
            batch_bytes = 0
            await asyncio.sleep(batch_delay)

    if media_files:
        await send_media_group(
            url_pool,
            token_pool,
            chat_id,
            media_files,
            topic_id=topic_id,
            max_retries=max_retries,
            retry_delay=retry_delay,
        )
        sent += len(media_files)
        sent_bytes += batch_bytes

    finished_at = datetime.now()
    elapsed_seconds = time.monotonic() - started_clock
    avg_seconds = elapsed_seconds / sent if sent > 0 else 0.0
    await send_message(
        url_pool,
        token_pool,
        chat_id,
        "Completed image upload from %s at %s (elapsed %s, avg/image %s, total %s, avg %s, sent %d, skipped %d)"
        % (
            image_dir,
            _format_timestamp(finished_at),
            _format_duration(elapsed_seconds),
            _format_duration(avg_seconds),
            _format_bytes(sent_bytes),
            _format_speed(sent_bytes, elapsed_seconds),
            sent,
            skipped,
        ),
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
    )
    _print_summary(
        "image",
        str(image_dir),
        started_at,
        finished_at,
        elapsed_seconds,
        sent,
        skipped,
        sent_bytes,
    )


async def send_mixed_from_paths(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    paths: list[Path],
    *,
    source_label: str,
    with_image: bool,
    with_video: bool,
    with_audio: bool,
    with_file: bool,
    group_size: int = 4,
    batch_delay: int = 3,
    progress: bool = True,
    include_globs: list[str] | None = None,
    exclude_globs: list[str] | None = None,
    enable_zip: bool = False,
    zip_passwords: list[str] | None = None,
    topic_id: int | None = None,
    max_retries: int = 3,
    retry_delay: int = 3,
) -> None:
    include_globs = include_globs or []
    exclude_globs = exclude_globs or []
    zip_passwords = zip_passwords or []

    entries: list[Path] = []
    zip_entries: set[Path] = set()
    for path in paths:
        rel_name = path.name
        if include_globs and not _matches_include(rel_name, include_globs):
            continue
        if _matches_exclude(rel_name, exclude_globs):
            continue
        if enable_zip and path.name.lower().endswith(".zip"):
            entries.append(path)
            zip_entries.add(path)
            continue
        send_type = _mixed_send_type(
            path.name,
            with_image=with_image,
            with_video=with_video,
            with_audio=with_audio,
            with_file=with_file,
        )
        if send_type:
            entries.append(path)

    if not entries:
        logging.info("No matching files found in %s", source_label)
        return

    started_at = datetime.now()
    started_clock = time.monotonic()
    sent = 0
    skipped = 0
    sent_bytes = 0
    await send_message(
        url_pool,
        token_pool,
        chat_id,
        f"Starting mixed upload from {source_label}: {len(entries)} file(s) at {_format_timestamp(started_at)}",
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
    )

    iterator = tqdm.tqdm(entries) if progress else entries
    media_files: list[tuple[str, bytes]] = []
    batch_bytes = 0
    for path in iterator:
        if path in zip_entries:
            if media_files:
                await send_media_group(
                    url_pool,
                    token_pool,
                    chat_id,
                    media_files,
                    topic_id=topic_id,
                    max_retries=max_retries,
                    retry_delay=retry_delay,
                )
                sent += len(media_files)
                sent_bytes += batch_bytes
                media_files = []
                batch_bytes = 0
                await asyncio.sleep(batch_delay)
            await send_mixed_from_zip(
                url_pool,
                token_pool,
                chat_id,
                path,
                with_image=with_image,
                with_video=with_video,
                with_audio=with_audio,
                with_file=with_file,
                group_size=group_size,
                batch_delay=batch_delay,
                progress=progress,
                include_globs=include_globs,
                exclude_globs=exclude_globs,
                zip_passwords=zip_passwords,
                topic_id=topic_id,
                max_retries=max_retries,
                retry_delay=retry_delay,
            )
            continue

        send_type = _mixed_send_type(
            path.name,
            with_image=with_image,
            with_video=with_video,
            with_audio=with_audio,
            with_file=with_file,
        )
        if not send_type:
            continue

        if send_type == "image":
            try:
                data = path.read_bytes()
            except OSError as exc:
                logging.warning("Failed to read %s: %s", path, exc)
                skipped += 1
                continue
            media_files.append((path.name, data))
            batch_bytes += len(data)
            if len(media_files) >= group_size:
                await send_media_group(
                    url_pool,
                    token_pool,
                    chat_id,
                    media_files,
                    topic_id=topic_id,
                    max_retries=max_retries,
                    retry_delay=retry_delay,
                )
                sent += len(media_files)
                sent_bytes += batch_bytes
                media_files = []
                batch_bytes = 0
                await asyncio.sleep(batch_delay)
            continue

        if media_files:
            await send_media_group(
                url_pool,
                token_pool,
                chat_id,
                media_files,
                topic_id=topic_id,
                max_retries=max_retries,
                retry_delay=retry_delay,
            )
            sent += len(media_files)
            sent_bytes += batch_bytes
            media_files = []
            batch_bytes = 0
            await asyncio.sleep(batch_delay)

        try:
            data = path.read_bytes()
        except OSError as exc:
            logging.warning("Failed to read %s: %s", path, exc)
            skipped += 1
            continue
        await _send_single_file(
            url_pool,
            token_pool,
            chat_id,
            send_type,
            path.name,
            data,
            topic_id=topic_id,
            max_retries=max_retries,
            retry_delay=retry_delay,
        )
        sent += 1
        sent_bytes += len(data)
        await asyncio.sleep(batch_delay)

    if media_files:
        await send_media_group(
            url_pool,
            token_pool,
            chat_id,
            media_files,
            topic_id=topic_id,
            max_retries=max_retries,
            retry_delay=retry_delay,
        )
        sent += len(media_files)
        sent_bytes += batch_bytes

    elapsed_seconds = time.monotonic() - started_clock
    avg_seconds = elapsed_seconds / sent if sent > 0 else 0.0
    finished_at = datetime.now()
    await send_message(
        url_pool,
        token_pool,
        chat_id,
        "Completed mixed upload from %s at %s (elapsed %s, avg/item %s, total %s, avg %s, sent %d, skipped %d)"
        % (
            source_label,
            _format_timestamp(finished_at),
            _format_duration(elapsed_seconds),
            _format_duration(avg_seconds),
            _format_bytes(sent_bytes),
            _format_speed(sent_bytes, elapsed_seconds),
            sent,
            skipped,
        ),
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
    )
    _print_summary(
        "mixed",
        source_label,
        started_at,
        finished_at,
        elapsed_seconds,
        sent,
        skipped,
        sent_bytes,
    )


async def send_mixed_from_dir(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    root_dir: Path,
    *,
    with_image: bool,
    with_video: bool,
    with_audio: bool,
    with_file: bool,
    group_size: int = 4,
    batch_delay: int = 3,
    progress: bool = True,
    include_globs: list[str] | None = None,
    exclude_globs: list[str] | None = None,
    enable_zip: bool = False,
    zip_passwords: list[str] | None = None,
    topic_id: int | None = None,
    max_retries: int = 3,
    retry_delay: int = 3,
) -> None:
    include_globs = include_globs or []
    exclude_globs = exclude_globs or []
    files = _collect_source_files(root_dir, include_globs, exclude_globs)
    if not files:
        logging.info("No matching files found in %s", root_dir)
        return
    await send_mixed_from_paths(
        url_pool,
        token_pool,
        chat_id,
        files,
        source_label=str(root_dir),
        with_image=with_image,
        with_video=with_video,
        with_audio=with_audio,
        with_file=with_file,
        group_size=group_size,
        batch_delay=batch_delay,
        progress=progress,
        include_globs=[],
        exclude_globs=[],
        enable_zip=enable_zip,
        zip_passwords=zip_passwords,
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
    )


async def send_mixed_from_zip(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    zip_file: Path,
    *,
    with_image: bool,
    with_video: bool,
    with_audio: bool,
    with_file: bool,
    group_size: int = 4,
    batch_delay: int = 3,
    progress: bool = True,
    include_globs: list[str] | None = None,
    exclude_globs: list[str] | None = None,
    zip_passwords: list[str] | None = None,
    topic_id: int | None = None,
    max_retries: int = 3,
    retry_delay: int = 3,
) -> None:
    include_globs = include_globs or []
    exclude_globs = exclude_globs or []
    zip_passwords = zip_passwords or []

    try:
        zip_ref = _open_zip_with_passwords(zip_file, zip_passwords)
    except RuntimeError as exc:
        await send_message(
            url_pool,
            token_pool,
            chat_id,
            f"Skipping zip (passwords failed): {zip_file.name}",
            topic_id=topic_id,
            max_retries=max_retries,
            retry_delay=retry_delay,
        )
        logging.warning("%s", exc)
        return

    with zip_ref:
        entries: list[str] = []
        for info in zip_ref.infolist():
            if info.is_dir():
                continue
            name = info.filename
            if not _matches_include(name, include_globs):
                continue
            if _matches_exclude(name, exclude_globs):
                continue
            send_type = _mixed_send_type(
                name,
                with_image=with_image,
                with_video=with_video,
                with_audio=with_audio,
                with_file=with_file,
            )
            if send_type:
                entries.append(name)

        if not entries:
            logging.info("No matching files found in %s", zip_file)
            return

        started_at = datetime.now()
        started_clock = time.monotonic()
        sent = 0
        skipped = 0
        sent_bytes = 0
        await send_message(
            url_pool,
            token_pool,
            chat_id,
            f"Starting mixed upload from {zip_file.name}: {len(entries)} file(s) at {_format_timestamp(started_at)}",
            topic_id=topic_id,
            max_retries=max_retries,
            retry_delay=retry_delay,
        )

        iterator = tqdm.tqdm(entries) if progress else entries
        media_files: list[tuple[str, bytes]] = []
        batch_bytes = 0
        for name in iterator:
            send_type = _mixed_send_type(
                name,
                with_image=with_image,
                with_video=with_video,
                with_audio=with_audio,
                with_file=with_file,
            )
            if not send_type:
                continue
            if send_type == "image":
                try:
                    with zip_ref.open(name) as handle:
                        data = handle.read()
                except OSError as exc:
                    logging.warning("Failed to read %s from %s: %s", name, zip_file, exc)
                    skipped += 1
                    continue
                media_files.append((Path(name).name, data))
                batch_bytes += len(data)
                if len(media_files) >= group_size:
                    await send_media_group(
                        url_pool,
                        token_pool,
                        chat_id,
                        media_files,
                        topic_id=topic_id,
                        max_retries=max_retries,
                        retry_delay=retry_delay,
                    )
                    sent += len(media_files)
                    sent_bytes += batch_bytes
                    media_files = []
                    batch_bytes = 0
                    await asyncio.sleep(batch_delay)
                continue

            if media_files:
                await send_media_group(
                    url_pool,
                    token_pool,
                    chat_id,
                    media_files,
                    topic_id=topic_id,
                    max_retries=max_retries,
                    retry_delay=retry_delay,
                )
                sent += len(media_files)
                sent_bytes += batch_bytes
                media_files = []
                batch_bytes = 0
                await asyncio.sleep(batch_delay)

            try:
                with zip_ref.open(name) as handle:
                    data = handle.read()
            except OSError as exc:
                logging.warning("Failed to read %s from %s: %s", name, zip_file, exc)
                skipped += 1
                continue
            await _send_single_file(
                url_pool,
                token_pool,
                chat_id,
                send_type,
                Path(name).name,
                data,
                topic_id=topic_id,
                max_retries=max_retries,
                retry_delay=retry_delay,
            )
            sent += 1
            sent_bytes += len(data)
            await asyncio.sleep(batch_delay)

        if media_files:
            await send_media_group(
                url_pool,
                token_pool,
                chat_id,
                media_files,
                topic_id=topic_id,
                max_retries=max_retries,
                retry_delay=retry_delay,
            )
            sent += len(media_files)
            sent_bytes += batch_bytes

    elapsed_seconds = time.monotonic() - started_clock
    avg_seconds = elapsed_seconds / sent if sent > 0 else 0.0
    finished_at = datetime.now()
    await send_message(
        url_pool,
        token_pool,
        chat_id,
        "Completed mixed upload from %s at %s (elapsed %s, avg/item %s, total %s, avg %s, sent %d, skipped %d)"
        % (
            zip_file.name,
            _format_timestamp(finished_at),
            _format_duration(elapsed_seconds),
            _format_duration(avg_seconds),
            _format_bytes(sent_bytes),
            _format_speed(sent_bytes, elapsed_seconds),
            sent,
            skipped,
        ),
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
    )
    _print_summary(
        "mixed",
        zip_file.name,
        started_at,
        finished_at,
        elapsed_seconds,
        sent,
        skipped,
        sent_bytes,
    )


async def send_files_from_dir(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    root_dir: Path,
    *,
    send_type: str,
    topic_id: int | None = None,
    start_index: int = 0,
    end_index: int = 0,
    batch_delay: int = 3,
    progress: bool = True,
    include_globs: list[str] | None = None,
    exclude_globs: list[str] | None = None,
    enable_zip: bool = False,
    zip_passwords: list[str] | None = None,
    max_retries: int = 3,
    retry_delay: int = 3,
) -> None:
    include_globs = include_globs or []
    exclude_globs = exclude_globs or []
    zip_passwords = zip_passwords or []
    allowed_exts = _allowed_exts_for_send_type(send_type)
    files = _collect_files(
        root_dir, include_globs, exclude_globs, enable_zip, allowed_exts
    )
    if not files:
        logging.info("No files found in %s", root_dir)
        return

    label = _send_type_label(send_type)
    started_at = datetime.now()
    started_clock = time.monotonic()
    sent = 0
    skipped = 0
    sent_bytes = 0
    await send_message(
        url_pool,
        token_pool,
        chat_id,
        f"Starting {label} upload: {len(files)} file(s) at {_format_timestamp(started_at)}",
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
    )

    min_index = start_index
    max_index = end_index if end_index else None
    iterator = tqdm.tqdm(files) if progress else files
    for idx, path in enumerate(iterator):
        if idx < min_index:
            continue
        if max_index is not None and idx >= max_index:
            break
        if path.name.lower().endswith(".zip") and enable_zip:
            await send_files_from_zip(
                url_pool,
                token_pool,
                chat_id,
                path,
                send_type=send_type,
                topic_id=topic_id,
                start_index=0,
                end_index=0,
                batch_delay=batch_delay,
                progress=progress,
                include_globs=include_globs,
                exclude_globs=exclude_globs,
                zip_passwords=zip_passwords,
                max_retries=max_retries,
                retry_delay=retry_delay,
            )
            continue
        try:
            data = path.read_bytes()
        except OSError as exc:
            logging.warning("Failed to read %s: %s", path, exc)
            skipped += 1
            continue
        await _send_single_file(
            url_pool,
            token_pool,
            chat_id,
            send_type,
            path.name,
            data,
            topic_id=topic_id,
            max_retries=max_retries,
            retry_delay=retry_delay,
        )
        sent += 1
        sent_bytes += len(data)
        await asyncio.sleep(batch_delay)

    finished_at = datetime.now()
    elapsed_seconds = time.monotonic() - started_clock
    avg_seconds = elapsed_seconds / sent if sent > 0 else 0.0
    await send_message(
        url_pool,
        token_pool,
        chat_id,
        "Completed %s upload from %s at %s (elapsed %s, avg/%s %s, total %s, avg %s, sent %d, skipped %d)"
        % (
            label,
            root_dir,
            _format_timestamp(finished_at),
            _format_duration(elapsed_seconds),
            label,
            _format_duration(avg_seconds),
            _format_bytes(sent_bytes),
            _format_speed(sent_bytes, elapsed_seconds),
            sent,
            skipped,
        ),
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
    )
    _print_summary(
        label,
        str(root_dir),
        started_at,
        finished_at,
        elapsed_seconds,
        sent,
        skipped,
        sent_bytes,
    )


async def send_files_from_zip(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    zip_file: Path,
    *,
    send_type: str,
    topic_id: int | None = None,
    start_index: int = 0,
    end_index: int = 0,
    batch_delay: int = 3,
    progress: bool = True,
    include_globs: list[str] | None = None,
    exclude_globs: list[str] | None = None,
    zip_passwords: list[str] | None = None,
    max_retries: int = 3,
    retry_delay: int = 3,
) -> None:
    include_globs = include_globs or []
    exclude_globs = exclude_globs or []
    zip_passwords = zip_passwords or []
    try:
        zip_ref = _open_zip_with_passwords(zip_file, zip_passwords)
    except RuntimeError as exc:
        await send_message(
            url_pool,
            token_pool,
            chat_id,
            f"Skipping zip (passwords failed): {zip_file.name}",
            topic_id=topic_id,
            max_retries=max_retries,
            retry_delay=retry_delay,
        )
        logging.warning("%s", exc)
        return

    allowed_exts = _allowed_exts_for_send_type(send_type)
    with zip_ref:
        names = [
            name
            for name in zip_ref.namelist()
            if not name.endswith("/")
            and _matches_include(name, include_globs)
            and not _matches_exclude(name, exclude_globs)
            and (allowed_exts is None or name.lower().endswith(allowed_exts))
        ]

        if not names:
            logging.info("No matching files found in %s", zip_file)
            return

        label = _send_type_label(send_type)
        started_at = datetime.now()
        started_clock = time.monotonic()
        sent = 0
        skipped = 0
        sent_bytes = 0
        await send_message(
            url_pool,
            token_pool,
            chat_id,
            f"Starting {label} upload: {len(names)} file(s) at {_format_timestamp(started_at)}",
            topic_id=topic_id,
            max_retries=max_retries,
            retry_delay=retry_delay,
        )

        min_index = start_index
        max_index = end_index if end_index else None
        iterator = tqdm.tqdm(names) if progress else names
        for idx, name in enumerate(iterator):
            if idx < min_index:
                continue
            if max_index is not None and idx >= max_index:
                break
            try:
                with zip_ref.open(name) as handle:
                    data = handle.read()
            except OSError as exc:
                logging.warning("Failed to read %s from %s: %s", name, zip_file, exc)
                skipped += 1
                continue
            await _send_single_file(
                url_pool,
                token_pool,
                chat_id,
                send_type,
                Path(name).name,
                data,
                topic_id=topic_id,
                max_retries=max_retries,
                retry_delay=retry_delay,
            )
            sent += 1
            sent_bytes += len(data)
            await asyncio.sleep(batch_delay)

    finished_at = datetime.now()
    elapsed_seconds = time.monotonic() - started_clock
    avg_seconds = elapsed_seconds / sent if sent > 0 else 0.0
    await send_message(
        url_pool,
        token_pool,
        chat_id,
        "Completed %s upload from %s at %s (elapsed %s, avg/%s %s, total %s, avg %s, sent %d, skipped %d)"
        % (
            label,
            zip_file.name,
            _format_timestamp(finished_at),
            _format_duration(elapsed_seconds),
            label,
            _format_duration(avg_seconds),
            _format_bytes(sent_bytes),
            _format_speed(sent_bytes, elapsed_seconds),
            sent,
            skipped,
        ),
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
    )
    _print_summary(
        label,
        zip_file.name,
        started_at,
        finished_at,
        elapsed_seconds,
        sent,
        skipped,
        sent_bytes,
    )


def _allowed_exts_for_send_type(send_type: str) -> tuple[str, ...] | None:
    if send_type == "video":
        return VIDEO_EXTENSIONS
    if send_type == "audio":
        return AUDIO_EXTENSIONS
    if send_type == "file":
        return None
    raise ValueError(f"Unsupported send_type: {send_type}")


def _send_type_label(send_type: str) -> str:
    if send_type == "video":
        return "video"
    if send_type == "audio":
        return "audio"
    if send_type == "file":
        return "file"
    return "file"


async def _send_single_file(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    send_type: str,
    filename: str,
    data: bytes,
    *,
    topic_id: int | None = None,
    max_retries: int = 3,
    retry_delay: int = 3,
) -> None:
    if send_type == "file":
        await send_document(
            url_pool,
            token_pool,
            chat_id,
            filename,
            data,
            topic_id=topic_id,
            max_retries=max_retries,
            retry_delay=retry_delay,
        )
        return
    if send_type == "video":
        await send_video(
            url_pool,
            token_pool,
            chat_id,
            filename,
            data,
            topic_id=topic_id,
            max_retries=max_retries,
            retry_delay=retry_delay,
        )
        return
    if send_type == "audio":
        await send_audio(
            url_pool,
            token_pool,
            chat_id,
            filename,
            data,
            topic_id=topic_id,
            max_retries=max_retries,
            retry_delay=retry_delay,
        )
        return
    raise ValueError(f"Unsupported send_type: {send_type}")


async def send_images_from_zip(
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    zip_file: Path,
    *,
    topic_id: int | None = None,
    group_size: int = 4,
    start_index: int = 0,
    end_index: int = 0,
    batch_delay: int = 3,
    progress: bool = True,
    include_globs: list[str] | None = None,
    exclude_globs: list[str] | None = None,
    zip_passwords: list[str] | None = None,
    max_retries: int = 3,
    retry_delay: int = 3,
) -> None:
    include_globs = include_globs or []
    exclude_globs = exclude_globs or []
    zip_passwords = zip_passwords or []
    try:
        zip_ref = _open_zip_with_passwords(zip_file, zip_passwords)
    except RuntimeError as exc:
        await send_message(
            url_pool,
            token_pool,
            chat_id,
            f"Skipping zip (passwords failed): {zip_file.name}",
            topic_id=topic_id,
            max_retries=max_retries,
            retry_delay=retry_delay,
        )
        logging.warning("%s", exc)
        return

    with zip_ref:
        image_names = [
            name
            for name in zip_ref.namelist()
            if name.lower().endswith(IMAGE_EXTENSIONS)
            and _matches_include(name, include_globs)
            and not _matches_exclude(name, exclude_globs)
        ]

        if not image_names:
            logging.info("No images found in %s", zip_file)
            return

        await send_message(
            url_pool,
            token_pool,
            chat_id,
            f"Starting image upload: {len(image_names)} file(s)",
            topic_id=topic_id,
            max_retries=max_retries,
            retry_delay=retry_delay,
        )

        min_index = start_index * group_size
        max_index = end_index * group_size if end_index else None
        media_files: list[tuple[str, bytes]] = []
        iterator = tqdm.tqdm(image_names) if progress else image_names
        for idx, name in enumerate(iterator):
            if idx < min_index:
                continue
            if max_index is not None and idx >= max_index:
                break

            with zip_ref.open(name) as image_file:
                media_files.append((Path(name).name, image_file.read()))
            if len(media_files) >= group_size:
                await send_media_group(
                    url_pool,
                    token_pool,
                    chat_id,
                    media_files,
                    topic_id=topic_id,
                    max_retries=max_retries,
                    retry_delay=retry_delay,
                )
                media_files = []
                await asyncio.sleep(batch_delay)

        if media_files:
            await send_media_group(
                url_pool,
                token_pool,
                chat_id,
                media_files,
                topic_id=topic_id,
                max_retries=max_retries,
                retry_delay=retry_delay,
            )

    await send_message(
        url_pool,
        token_pool,
        chat_id,
        f"Completed image upload from {zip_file.name}",
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
    )


def _open_zip_with_passwords(zip_file: Path, passwords: list[str]) -> zipfile.ZipFile:
    zip_ref = zipfile.ZipFile(zip_file, "r")
    if not zip_ref.namelist():
        return zip_ref
    if not passwords:
        return zip_ref
    target_name = next((name for name in zip_ref.namelist()), None)
    if not target_name:
        return zip_ref
    for password in passwords:
        try:
            with zip_ref.open(target_name, pwd=password.encode("utf-8")) as handle:
                handle.read(1)
            zip_ref.setpassword(password.encode("utf-8"))
            return zip_ref
        except RuntimeError:
            continue
    zip_ref.close()
    raise RuntimeError(f"Failed to open zip (passwords exhausted): {zip_file}")


def open_zip_entry(
    zip_file: Path, inner_path: str, passwords: list[str]
) -> tuple[bytes, str]:
    try:
        with zipfile.ZipFile(zip_file, "r") as zip_ref:
            try:
                with zip_ref.open(inner_path) as handle:
                    return handle.read(), Path(inner_path).name
            except RuntimeError:
                if not passwords:
                    raise
            for password in passwords:
                try:
                    with zip_ref.open(
                        inner_path, pwd=password.encode("utf-8")
                    ) as handle:
                        return handle.read(), Path(inner_path).name
                except RuntimeError:
                    continue
    except KeyError as exc:
        raise FileNotFoundError(
            f"Zip entry not found: {zip_file}::{inner_path}"
        ) from exc
    raise RuntimeError(
        f"Failed to open zip entry (passwords exhausted): {zip_file}::{inner_path}"
    )
