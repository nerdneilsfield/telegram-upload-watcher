import asyncio
import json
import logging
import mimetypes
import os
import zipfile
from pathlib import Path

import aiohttp
import requests
import tqdm

from .constants import IMAGE_EXTENSIONS
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


def _collect_image_files(image_dir: Path) -> list[Path]:
    files: list[Path] = []
    for root, _, filenames in os.walk(image_dir):
        for filename in filenames:
            if filename.lower().endswith(IMAGE_EXTENSIONS):
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
    max_retries: int = 3,
    retry_delay: int = 3,
) -> None:
    files = _collect_image_files(image_dir)
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
    max_retries: int = 3,
    retry_delay: int = 3,
) -> None:
    with zipfile.ZipFile(zip_file, "r") as zip_ref:
        image_names = [
            name
            for name in zip_ref.namelist()
            if name.lower().endswith(IMAGE_EXTENSIONS)
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
