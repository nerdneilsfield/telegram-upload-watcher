import asyncio
import fnmatch
import json
import logging
import mimetypes
import os
import zipfile
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

    await send_message(
        url_pool,
        token_pool,
        chat_id,
        f"Starting image upload: {len(files)} file(s)",
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
    )

    min_index = start_index * group_size
    max_index = end_index * group_size if end_index else None
    media_files: list[tuple[str, bytes]] = []
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
        with path.open("rb") as image_file:
            media_files.append((path.name, image_file.read()))
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
        f"Completed image upload from {image_dir}",
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
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
    await send_message(
        url_pool,
        token_pool,
        chat_id,
        f"Starting {label} upload: {len(files)} file(s)",
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
        await _send_single_file(
            url_pool,
            token_pool,
            chat_id,
            send_type,
            path.name,
            path.read_bytes(),
            topic_id=topic_id,
            max_retries=max_retries,
            retry_delay=retry_delay,
        )
        await asyncio.sleep(batch_delay)

    await send_message(
        url_pool,
        token_pool,
        chat_id,
        f"Completed {label} upload from {root_dir}",
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
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
        await send_message(
            url_pool,
            token_pool,
            chat_id,
            f"Starting {label} upload: {len(names)} file(s)",
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
            with zip_ref.open(name) as handle:
                await _send_single_file(
                    url_pool,
                    token_pool,
                    chat_id,
                    send_type,
                    Path(name).name,
                    handle.read(),
                    topic_id=topic_id,
                    max_retries=max_retries,
                    retry_delay=retry_delay,
                )
            await asyncio.sleep(batch_delay)

    await send_message(
        url_pool,
        token_pool,
        chat_id,
        f"Completed {label} upload from {zip_file.name}",
        topic_id=topic_id,
        max_retries=max_retries,
        retry_delay=retry_delay,
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
