import argparse
import asyncio
import logging
import time
from datetime import datetime
from pathlib import Path

from .config import load_config
from .constants import IMAGE_EXTENSIONS
from .pools import TokenPool, UrlPool
from .queue import JsonlQueue, QUEUE_META_TYPE, QUEUE_META_VERSION, build_source_fingerprint
from .notify import NotifyConfig, notify_loop
from .sender import SenderConfig, drain_queue, sender_loop
from .telegram import (
    _allowed_exts_for_send_type,
    _collect_files,
    _collect_image_files,
    _collect_source_files,
    _format_bytes,
    _format_duration,
    _format_speed,
    _format_timestamp,
    _matches_exclude,
    _matches_include,
    _mixed_send_type,
    _open_zip_with_passwords,
    _print_summary,
    _send_type_label,
    send_audio,
    send_document,
    send_files_from_dir,
    send_files_from_zip,
    send_images_from_dir,
    send_images_from_zip,
    send_mixed_from_dir,
    send_mixed_from_paths,
    send_mixed_from_zip,
    send_message,
    send_video,
    test_token,
)
from .watcher import WatchConfig, watch_loop


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Send messages or images to Telegram")
    parser.add_argument(
        "--log-level",
        default="INFO",
        help="Logging level (DEBUG, INFO, WARNING, ERROR)",
    )

    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("--config", type=Path, help="Path to INI config file")
    common.add_argument(
        "--bot-token",
        type=str,
        help="Telegram bot token(s), comma-separated",
    )
    common.add_argument(
        "--api-url",
        type=str,
        help="Telegram API base URL(s), comma-separated",
    )
    common.add_argument(
        "--chat-id",
        type=str,
        required=True,
        help="Target chat ID (channel/group/user)",
    )
    common.add_argument(
        "--topic-id",
        type=int,
        help="Topic/thread ID inside a forum group/channel",
    )
    common.add_argument(
        "--validate-tokens",
        action="store_true",
        help="Validate tokens via getMe before sending",
    )
    common.add_argument(
        "--max-retries",
        type=int,
        default=3,
        help="Maximum retries for Telegram API calls",
    )
    common.add_argument(
        "--retry-delay",
        type=int,
        default=3,
        help="Delay between retries in seconds",
    )

    subparsers = parser.add_subparsers(dest="command", required=True)

    send_message_parser = subparsers.add_parser(
        "send-message", parents=[common], help="Send a text message"
    )
    send_message_parser.add_argument(
        "--message", type=str, required=True, help="Message text to send"
    )

    send_images_parser = subparsers.add_parser(
        "send-images", parents=[common], help="Send images from a directory or zip"
    )
    send_images_parser.add_argument(
        "--image-dir",
        type=Path,
        action="append",
        default=[],
        help="Image directory (repeatable)",
    )
    send_images_parser.add_argument(
        "--zip-file",
        type=Path,
        action="append",
        default=[],
        help="Zip file path (repeatable)",
    )
    send_images_parser.add_argument(
        "--group-size",
        type=int,
        default=4,
        help="Images per media group",
    )
    send_images_parser.add_argument(
        "--start-index",
        type=int,
        default=0,
        help="Start group index (0-based)",
    )
    send_images_parser.add_argument(
        "--end-index",
        type=int,
        default=0,
        help="End group index (0 for no limit)",
    )
    send_images_parser.add_argument(
        "--batch-delay",
        type=int,
        default=3,
        help="Delay between media groups in seconds",
    )
    send_images_parser.add_argument(
        "--no-progress",
        action="store_true",
        help="Disable progress output",
    )
    send_images_parser.add_argument(
        "--enable-zip",
        action="store_true",
        help="Process zip files when scanning directories",
    )
    send_images_parser.add_argument(
        "--include",
        action="append",
        default=[],
        help="Glob pattern to include (repeatable or comma-separated)",
    )
    send_images_parser.add_argument(
        "--exclude",
        action="append",
        default=[],
        help="Glob pattern to exclude (repeatable or comma-separated)",
    )
    send_images_parser.add_argument(
        "--zip-pass",
        action="append",
        default=[],
        help="Zip password (repeatable)",
    )
    send_images_parser.add_argument(
        "--zip-pass-file",
        type=Path,
        help="Path to file with zip passwords (one per line)",
    )
    send_images_parser.add_argument(
        "--queue-file",
        type=Path,
        help="Path to JSONL queue file (enables resume mode)",
    )
    send_images_parser.add_argument(
        "--queue-retries",
        type=int,
        default=3,
        help="Maximum queue retry attempts per item",
    )

    send_file_parser = subparsers.add_parser(
        "send-file", parents=[common], help="Send files using sendDocument"
    )
    send_file_parser.add_argument(
        "--file",
        type=Path,
        action="append",
        default=[],
        help="File path (repeatable)",
    )
    send_file_parser.add_argument(
        "--dir",
        type=Path,
        action="append",
        default=[],
        help="Directory path (repeatable)",
    )
    send_file_parser.add_argument(
        "--zip-file",
        type=Path,
        action="append",
        default=[],
        help="Zip file path (repeatable)",
    )
    send_file_parser.add_argument(
        "--start-index",
        type=int,
        default=0,
        help="Start index (0-based)",
    )
    send_file_parser.add_argument(
        "--end-index",
        type=int,
        default=0,
        help="End index (0 for no limit)",
    )
    send_file_parser.add_argument(
        "--batch-delay",
        type=int,
        default=3,
        help="Delay between sends in seconds",
    )
    send_file_parser.add_argument(
        "--no-progress",
        action="store_true",
        help="Disable progress output",
    )
    send_file_parser.add_argument(
        "--enable-zip",
        action="store_true",
        help="Process zip files when scanning directories",
    )
    send_file_parser.add_argument(
        "--include",
        action="append",
        default=[],
        help="Glob pattern to include (repeatable or comma-separated)",
    )
    send_file_parser.add_argument(
        "--exclude",
        action="append",
        default=[],
        help="Glob pattern to exclude (repeatable or comma-separated)",
    )
    send_file_parser.add_argument(
        "--zip-pass",
        action="append",
        default=[],
        help="Zip password (repeatable)",
    )
    send_file_parser.add_argument(
        "--zip-pass-file",
        type=Path,
        help="Path to file with zip passwords (one per line)",
    )
    send_file_parser.add_argument(
        "--queue-file",
        type=Path,
        help="Path to JSONL queue file (enables resume mode)",
    )
    send_file_parser.add_argument(
        "--queue-retries",
        type=int,
        default=3,
        help="Maximum queue retry attempts per item",
    )

    send_video_parser = subparsers.add_parser(
        "send-video", parents=[common], help="Send videos using sendVideo"
    )
    send_video_parser.add_argument(
        "--file",
        type=Path,
        action="append",
        default=[],
        help="Video file path (repeatable)",
    )
    send_video_parser.add_argument(
        "--dir",
        type=Path,
        action="append",
        default=[],
        help="Directory path (repeatable)",
    )
    send_video_parser.add_argument(
        "--zip-file",
        type=Path,
        action="append",
        default=[],
        help="Zip file path (repeatable)",
    )
    send_video_parser.add_argument(
        "--start-index",
        type=int,
        default=0,
        help="Start index (0-based)",
    )
    send_video_parser.add_argument(
        "--end-index",
        type=int,
        default=0,
        help="End index (0 for no limit)",
    )
    send_video_parser.add_argument(
        "--batch-delay",
        type=int,
        default=3,
        help="Delay between sends in seconds",
    )
    send_video_parser.add_argument(
        "--no-progress",
        action="store_true",
        help="Disable progress output",
    )
    send_video_parser.add_argument(
        "--enable-zip",
        action="store_true",
        help="Process zip files when scanning directories",
    )
    send_video_parser.add_argument(
        "--include",
        action="append",
        default=[],
        help="Glob pattern to include (repeatable or comma-separated)",
    )
    send_video_parser.add_argument(
        "--exclude",
        action="append",
        default=[],
        help="Glob pattern to exclude (repeatable or comma-separated)",
    )
    send_video_parser.add_argument(
        "--zip-pass",
        action="append",
        default=[],
        help="Zip password (repeatable)",
    )
    send_video_parser.add_argument(
        "--zip-pass-file",
        type=Path,
        help="Path to file with zip passwords (one per line)",
    )
    send_video_parser.add_argument(
        "--queue-file",
        type=Path,
        help="Path to JSONL queue file (enables resume mode)",
    )
    send_video_parser.add_argument(
        "--queue-retries",
        type=int,
        default=3,
        help="Maximum queue retry attempts per item",
    )

    send_audio_parser = subparsers.add_parser(
        "send-audio", parents=[common], help="Send audio using sendAudio"
    )
    send_audio_parser.add_argument(
        "--file",
        type=Path,
        action="append",
        default=[],
        help="Audio file path (repeatable)",
    )
    send_audio_parser.add_argument(
        "--dir",
        type=Path,
        action="append",
        default=[],
        help="Directory path (repeatable)",
    )
    send_audio_parser.add_argument(
        "--zip-file",
        type=Path,
        action="append",
        default=[],
        help="Zip file path (repeatable)",
    )
    send_audio_parser.add_argument(
        "--start-index",
        type=int,
        default=0,
        help="Start index (0-based)",
    )
    send_audio_parser.add_argument(
        "--end-index",
        type=int,
        default=0,
        help="End index (0 for no limit)",
    )
    send_audio_parser.add_argument(
        "--batch-delay",
        type=int,
        default=3,
        help="Delay between sends in seconds",
    )
    send_audio_parser.add_argument(
        "--no-progress",
        action="store_true",
        help="Disable progress output",
    )
    send_audio_parser.add_argument(
        "--enable-zip",
        action="store_true",
        help="Process zip files when scanning directories",
    )
    send_audio_parser.add_argument(
        "--include",
        action="append",
        default=[],
        help="Glob pattern to include (repeatable or comma-separated)",
    )
    send_audio_parser.add_argument(
        "--exclude",
        action="append",
        default=[],
        help="Glob pattern to exclude (repeatable or comma-separated)",
    )
    send_audio_parser.add_argument(
        "--zip-pass",
        action="append",
        default=[],
        help="Zip password (repeatable)",
    )
    send_audio_parser.add_argument(
        "--zip-pass-file",
        type=Path,
        help="Path to file with zip passwords (one per line)",
    )
    send_audio_parser.add_argument(
        "--queue-file",
        type=Path,
        help="Path to JSONL queue file (enables resume mode)",
    )
    send_audio_parser.add_argument(
        "--queue-retries",
        type=int,
        default=3,
        help="Maximum queue retry attempts per item",
    )

    send_mixed_parser = subparsers.add_parser(
        "send-mixed", parents=[common], help="Send mixed media from files/dirs/zips"
    )
    send_mixed_parser.add_argument(
        "--file",
        type=Path,
        action="append",
        default=[],
        help="File path (repeatable)",
    )
    send_mixed_parser.add_argument(
        "--dir",
        type=Path,
        action="append",
        default=[],
        help="Directory path (repeatable)",
    )
    send_mixed_parser.add_argument(
        "--zip-file",
        type=Path,
        action="append",
        default=[],
        help="Zip file path (repeatable)",
    )
    send_mixed_parser.add_argument(
        "--group-size",
        type=int,
        default=4,
        help="Images per media group",
    )
    send_mixed_parser.add_argument(
        "--batch-delay",
        type=int,
        default=3,
        help="Delay between sends in seconds",
    )
    send_mixed_parser.add_argument(
        "--no-progress",
        action="store_true",
        help="Disable progress output",
    )
    send_mixed_parser.add_argument(
        "--enable-zip",
        action="store_true",
        help="Process zip files when scanning directories",
    )
    send_mixed_parser.add_argument(
        "--include",
        action="append",
        default=[],
        help="Glob pattern to include (repeatable or comma-separated)",
    )
    send_mixed_parser.add_argument(
        "--exclude",
        action="append",
        default=[],
        help="Glob pattern to exclude (repeatable or comma-separated)",
    )
    send_mixed_parser.add_argument(
        "--zip-pass",
        action="append",
        default=[],
        help="Zip password (repeatable)",
    )
    send_mixed_parser.add_argument(
        "--zip-pass-file",
        type=Path,
        help="Path to file with zip passwords (one per line)",
    )
    send_mixed_parser.add_argument(
        "--queue-file",
        type=Path,
        help="Path to JSONL queue file (enables resume mode)",
    )
    send_mixed_parser.add_argument(
        "--queue-retries",
        type=int,
        default=3,
        help="Maximum queue retry attempts per item",
    )
    send_mixed_parser.add_argument(
        "--with-image",
        action="store_true",
        help="Send matching images (media groups)",
    )
    send_mixed_parser.add_argument(
        "--with-video",
        action="store_true",
        help="Send matching videos",
    )
    send_mixed_parser.add_argument(
        "--with-audio",
        action="store_true",
        help="Send matching audio files",
    )
    send_mixed_parser.add_argument(
        "--with-file",
        action="store_true",
        help="Send other files as documents",
    )

    watch_parser = subparsers.add_parser(
        "watch", parents=[common], help="Watch folder and send queued images"
    )
    watch_parser.add_argument(
        "--watch-dir",
        type=Path,
        action="append",
        default=[],
        required=True,
        help="Folder to watch (repeatable)",
    )
    watch_parser.add_argument(
        "--queue-file",
        type=Path,
        default=Path("queue.jsonl"),
        help="Path to JSONL queue file",
    )
    watch_parser.add_argument(
        "--recursive",
        action="store_true",
        help="Enable recursive scan",
    )
    watch_parser.add_argument(
        "--with-image",
        action="store_true",
        help="Send matching images (media groups)",
    )
    watch_parser.add_argument(
        "--with-video",
        action="store_true",
        help="Send matching videos",
    )
    watch_parser.add_argument(
        "--with-audio",
        action="store_true",
        help="Send matching audio files",
    )
    watch_parser.add_argument(
        "--all",
        action="store_true",
        help="Send all matching files (images use media groups)",
    )
    watch_parser.add_argument(
        "--exclude",
        action="append",
        default=[],
        help="Glob pattern to exclude (repeatable or comma-separated)",
    )
    watch_parser.add_argument(
        "--include",
        action="append",
        default=[],
        help="Glob pattern to include (repeatable or comma-separated)",
    )
    watch_parser.add_argument(
        "--zip-pass",
        action="append",
        default=[],
        help="Zip password (repeatable)",
    )
    watch_parser.add_argument(
        "--zip-pass-file",
        type=Path,
        help="Path to file with zip passwords (one per line)",
    )
    watch_parser.add_argument(
        "--scan-interval",
        type=int,
        default=30,
        help="Folder scan interval in seconds",
    )
    watch_parser.add_argument(
        "--send-interval",
        type=int,
        default=30,
        help="Queue send interval in seconds",
    )
    watch_parser.add_argument(
        "--settle-seconds",
        type=int,
        default=5,
        help="Seconds to wait for file stability",
    )
    watch_parser.add_argument(
        "--group-size",
        type=int,
        default=4,
        help="Images per media group",
    )
    watch_parser.add_argument(
        "--batch-delay",
        type=int,
        default=3,
        help="Delay between media groups in seconds",
    )
    watch_parser.add_argument(
        "--pause-every",
        type=int,
        default=0,
        help="Pause after sending this many images (0 disables)",
    )
    watch_parser.add_argument(
        "--pause-seconds",
        type=int,
        default=0,
        help="Pause duration in seconds after reaching pause-every",
    )
    watch_parser.add_argument(
        "--notify",
        action="store_true",
        help="Send watch start/status/idle notifications",
    )
    watch_parser.add_argument(
        "--notify-interval",
        type=int,
        default=300,
        help="Seconds between status notifications",
    )
    watch_parser.add_argument(
        "--max-dimension",
        type=int,
        default=2000,
        help="Maximum image dimension before scaling",
    )
    watch_parser.add_argument(
        "--max-bytes",
        type=int,
        default=5 * 1024 * 1024,
        help="Maximum image size in bytes before PNG compression",
    )
    watch_parser.add_argument(
        "--png-start-level",
        type=int,
        default=8,
        help="Initial PNG compress level for greedy search (0-9)",
    )

    return parser


def _resolve_config(args: argparse.Namespace) -> tuple[list[str], list[str]]:
    api_urls: list[str] = []
    tokens: list[str] = []

    if args.config:
        if not args.config.exists():
            raise SystemExit(f"Config file not found: {args.config}")
        api_urls, tokens = load_config(args.config)

    if args.api_url:
        api_urls = [url.strip() for url in args.api_url.split(",") if url.strip()]

    if args.bot_token:
        tokens = [
            token.strip() for token in args.bot_token.split(",") if token.strip()
        ]

    if not api_urls:
        api_urls = ["https://api.telegram.org"]

    if not tokens:
        raise SystemExit("No bot token provided (use --bot-token or --config)")

    if args.validate_tokens:
        url_pool = UrlPool(api_urls)
        valid_tokens: list[str] = []
        for token in tokens:
            api_url = url_pool.get_url()
            if api_url and test_token(api_url, token):
                valid_tokens.append(token)
            url_pool.increment_url(api_url)
        if not valid_tokens:
            raise SystemExit("No valid tokens after validation")
        tokens = valid_tokens

    return api_urls, tokens


def _normalize_excludes(excludes: list[str]) -> list[str]:
    patterns: list[str] = []
    for item in excludes:
        for part in item.split(","):
            part = part.strip()
            if part:
                patterns.append(part)
    return patterns


def _normalize_includes(includes: list[str]) -> list[str]:
    return _normalize_excludes(includes)


def _load_zip_passwords(values: list[str], path: Path | None) -> list[str]:
    passwords: list[str] = []
    for value in values:
        value = value.strip()
        if value:
            passwords.append(value)
    if path:
        if not path.exists():
            raise SystemExit(f"Zip password file not found: {path}")
        for line in path.read_text(encoding="utf-8").splitlines():
            line = line.strip()
            if line:
                passwords.append(line)
    return passwords


def _validate_queue_retries(value: int) -> int:
    if value < 1:
        raise SystemExit("--queue-retries must be >= 1")
    return value


def _resolve_paths(paths: list[Path]) -> list[Path]:
    return [path.resolve() for path in paths]


def _enqueue_file_item(
    queue: JsonlQueue, path: Path, send_type: str
) -> int:
    try:
        info = path.stat()
    except OSError as exc:
        logging.warning("Failed to stat %s: %s", path, exc)
        return 0
    mtime_ns = getattr(info, "st_mtime_ns", None)
    source_fingerprint = build_source_fingerprint(str(path), info.st_size, mtime_ns)
    item = queue.enqueue_item(
        source_type="file",
        source_path=str(path),
        source_fingerprint=source_fingerprint,
        path=str(path),
        inner_path=None,
        size=info.st_size,
        mtime_ns=mtime_ns,
        crc=None,
        send_type=send_type,
    )
    return 1 if item else 0


def _enqueue_zip_images(
    queue: JsonlQueue,
    zip_file: Path,
    include_globs: list[str],
    exclude_globs: list[str],
    start_index: int,
    end_index: int,
    group_size: int,
    zip_passwords: list[str],
) -> int:
    try:
        zip_ref = _open_zip_with_passwords(zip_file, zip_passwords)
    except RuntimeError as exc:
        logging.warning("%s", exc)
        return 0
    try:
        source_info = zip_file.stat()
    except OSError as exc:
        logging.warning("Failed to stat %s: %s", zip_file, exc)
        zip_ref.close()
        return 0

    mtime_ns = getattr(source_info, "st_mtime_ns", None)
    source_fingerprint = build_source_fingerprint(
        str(zip_file), source_info.st_size, mtime_ns
    )
    enqueued = 0
    with zip_ref:
        entries = [
            info
            for info in zip_ref.infolist()
            if not info.is_dir()
            and info.filename.lower().endswith(IMAGE_EXTENSIONS)
            and _matches_include(info.filename, include_globs)
            and not _matches_exclude(info.filename, exclude_globs)
        ]
        if not entries:
            logging.info("No images found in %s", zip_file)
            return 0

        min_index = start_index * group_size
        max_index = end_index * group_size if end_index else None
        selected = entries[min_index:max_index]
        for info in selected:
            item = queue.enqueue_item(
                source_type="zip",
                source_path=str(zip_file),
                source_fingerprint=source_fingerprint,
                path=str(zip_file),
                inner_path=info.filename,
                size=info.file_size,
                mtime_ns=None,
                crc=info.CRC,
                send_type="image",
            )
            if item:
                enqueued += 1
    return enqueued


def _enqueue_zip_files(
    queue: JsonlQueue,
    zip_file: Path,
    send_type: str,
    include_globs: list[str],
    exclude_globs: list[str],
    start_index: int,
    end_index: int,
    zip_passwords: list[str],
) -> int:
    try:
        zip_ref = _open_zip_with_passwords(zip_file, zip_passwords)
    except RuntimeError as exc:
        logging.warning("%s", exc)
        return 0
    try:
        source_info = zip_file.stat()
    except OSError as exc:
        logging.warning("Failed to stat %s: %s", zip_file, exc)
        zip_ref.close()
        return 0

    allowed_exts = _allowed_exts_for_send_type(send_type)
    mtime_ns = getattr(source_info, "st_mtime_ns", None)
    source_fingerprint = build_source_fingerprint(
        str(zip_file), source_info.st_size, mtime_ns
    )
    enqueued = 0
    with zip_ref:
        entries = [
            info
            for info in zip_ref.infolist()
            if not info.is_dir()
            and _matches_include(info.filename, include_globs)
            and not _matches_exclude(info.filename, exclude_globs)
            and (
                allowed_exts is None
                or info.filename.lower().endswith(allowed_exts)
            )
        ]
        if not entries:
            logging.info("No matching files found in %s", zip_file)
            return 0

        min_index = start_index
        max_index = end_index if end_index else None
        selected = entries[min_index:max_index]
        for info in selected:
            item = queue.enqueue_item(
                source_type="zip",
                source_path=str(zip_file),
                source_fingerprint=source_fingerprint,
                path=str(zip_file),
                inner_path=info.filename,
                size=info.file_size,
                mtime_ns=None,
                crc=info.CRC,
                send_type=send_type,
            )
            if item:
                enqueued += 1
    return enqueued


def _enqueue_zip_mixed(
    queue: JsonlQueue,
    zip_file: Path,
    include_globs: list[str],
    exclude_globs: list[str],
    *,
    with_image: bool,
    with_video: bool,
    with_audio: bool,
    with_file: bool,
    zip_passwords: list[str],
) -> int:
    try:
        zip_ref = _open_zip_with_passwords(zip_file, zip_passwords)
    except RuntimeError as exc:
        logging.warning("%s", exc)
        return 0
    try:
        source_info = zip_file.stat()
    except OSError as exc:
        logging.warning("Failed to stat %s: %s", zip_file, exc)
        zip_ref.close()
        return 0

    mtime_ns = getattr(source_info, "st_mtime_ns", None)
    source_fingerprint = build_source_fingerprint(
        str(zip_file), source_info.st_size, mtime_ns
    )
    enqueued = 0
    with zip_ref:
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
            if not send_type:
                continue
            item = queue.enqueue_item(
                source_type="zip",
                source_path=str(zip_file),
                source_fingerprint=source_fingerprint,
                path=str(zip_file),
                inner_path=name,
                size=info.file_size,
                mtime_ns=None,
                crc=info.CRC,
                send_type=send_type,
            )
            if item:
                enqueued += 1
    return enqueued


def _enqueue_images_from_dir(
    queue: JsonlQueue,
    image_dir: Path,
    include_globs: list[str],
    exclude_globs: list[str],
    *,
    enable_zip: bool,
    start_index: int,
    end_index: int,
    group_size: int,
    zip_passwords: list[str],
) -> int:
    files = _collect_image_files(
        image_dir, include_globs, exclude_globs, enable_zip
    )
    if not files:
        logging.info("No images found in %s", image_dir)
        return 0

    min_index = start_index * group_size
    max_index = end_index * group_size if end_index else None
    selected = files[min_index:max_index]
    enqueued = 0
    for path in selected:
        if enable_zip and path.name.lower().endswith(".zip"):
            enqueued += _enqueue_zip_images(
                queue,
                path,
                include_globs,
                exclude_globs,
                start_index=0,
                end_index=0,
                group_size=group_size,
                zip_passwords=zip_passwords,
            )
            continue
        enqueued += _enqueue_file_item(queue, path, "image")
    return enqueued


def _enqueue_files_from_dir(
    queue: JsonlQueue,
    root_dir: Path,
    send_type: str,
    include_globs: list[str],
    exclude_globs: list[str],
    *,
    enable_zip: bool,
    start_index: int,
    end_index: int,
    zip_passwords: list[str],
) -> int:
    allowed_exts = _allowed_exts_for_send_type(send_type)
    files = _collect_files(
        root_dir, include_globs, exclude_globs, enable_zip, allowed_exts
    )
    if not files:
        logging.info("No files found in %s", root_dir)
        return 0

    min_index = start_index
    max_index = end_index if end_index else None
    selected = files[min_index:max_index]
    enqueued = 0
    for path in selected:
        if enable_zip and path.name.lower().endswith(".zip"):
            enqueued += _enqueue_zip_files(
                queue,
                path,
                send_type,
                include_globs,
                exclude_globs,
                start_index=0,
                end_index=0,
                zip_passwords=zip_passwords,
            )
            continue
        enqueued += _enqueue_file_item(queue, path, send_type)
    return enqueued


def _enqueue_mixed_from_paths(
    queue: JsonlQueue,
    paths: list[Path],
    include_globs: list[str],
    exclude_globs: list[str],
    *,
    with_image: bool,
    with_video: bool,
    with_audio: bool,
    with_file: bool,
    enable_zip: bool,
    zip_passwords: list[str],
    apply_filters: bool,
) -> int:
    enqueued = 0
    for path in paths:
        name = path.name
        if apply_filters:
            if include_globs and not _matches_include(name, include_globs):
                continue
            if _matches_exclude(name, exclude_globs):
                continue
        if enable_zip and name.lower().endswith(".zip"):
            enqueued += _enqueue_zip_mixed(
                queue,
                path,
                include_globs,
                exclude_globs,
                with_image=with_image,
                with_video=with_video,
                with_audio=with_audio,
                with_file=with_file,
                zip_passwords=zip_passwords,
            )
            continue
        send_type = _mixed_send_type(
            name,
            with_image=with_image,
            with_video=with_video,
            with_audio=with_audio,
            with_file=with_file,
        )
        if not send_type:
            continue
        enqueued += _enqueue_file_item(queue, path, send_type)
    return enqueued


async def run_command(args: argparse.Namespace) -> None:
    api_urls, tokens = _resolve_config(args)
    url_pool = UrlPool(api_urls)
    token_pool = TokenPool(tokens)

    if args.command == "send-message":
        await send_message(
            url_pool,
            token_pool,
            args.chat_id,
            args.message,
            topic_id=args.topic_id,
            max_retries=args.max_retries,
            retry_delay=args.retry_delay,
        )
        return

    if args.command == "send-images":
        image_dirs = args.image_dir or []
        zip_files = args.zip_file or []
        if not image_dirs and not zip_files:
            raise SystemExit("Provide --image-dir or --zip-file")

        include_globs = _normalize_includes(args.include)
        exclude_globs = _normalize_excludes(args.exclude)
        zip_passwords = _load_zip_passwords(args.zip_pass, args.zip_pass_file)

        if args.queue_file:
            queue_retries = _validate_queue_retries(args.queue_retries)
            resolved_dirs = _resolve_paths(image_dirs)
            resolved_zips = _resolve_paths(zip_files)
            meta = {
                "type": QUEUE_META_TYPE,
                "version": QUEUE_META_VERSION,
                "params": {
                    "command": "send-images",
                    "chat_id": args.chat_id,
                    "topic_id": args.topic_id,
                    "dirs": [str(path) for path in resolved_dirs],
                    "zip_files": [str(path) for path in resolved_zips],
                    "include": include_globs,
                    "exclude": exclude_globs,
                    "start_index": args.start_index,
                    "end_index": args.end_index,
                    "group_size": args.group_size,
                    "batch_delay": args.batch_delay,
                    "enable_zip": args.enable_zip,
                    "queue_retries": queue_retries,
                    "max_retries": args.max_retries,
                    "retry_delay": args.retry_delay,
                    "max_dimension": args.max_dimension,
                    "max_bytes": args.max_bytes,
                    "png_start_level": args.png_start_level,
                },
            }
            try:
                queue = JsonlQueue(args.queue_file, meta=meta)
            except ValueError as exc:
                raise SystemExit(str(exc)) from exc

            for image_dir in resolved_dirs:
                if not image_dir.exists():
                    raise SystemExit(f"Image directory not found: {image_dir}")
                _enqueue_images_from_dir(
                    queue,
                    image_dir,
                    include_globs,
                    exclude_globs,
                    enable_zip=args.enable_zip,
                    start_index=args.start_index,
                    end_index=args.end_index,
                    group_size=args.group_size,
                    zip_passwords=zip_passwords,
                )
            for zip_file in resolved_zips:
                if not zip_file.exists():
                    raise SystemExit(f"Zip file not found: {zip_file}")
                _enqueue_zip_images(
                    queue,
                    zip_file,
                    include_globs,
                    exclude_globs,
                    start_index=args.start_index,
                    end_index=args.end_index,
                    group_size=args.group_size,
                    zip_passwords=zip_passwords,
                )

            pending = queue.get_pending(max_attempts=queue_retries)
            if not pending:
                logging.info("No queued images to send.")
                return

            started_at = datetime.now()
            started_clock = time.monotonic()
            await send_message(
                url_pool,
                token_pool,
                args.chat_id,
                f"Starting image upload from queue: {len(pending)} file(s) at {_format_timestamp(started_at)}",
                topic_id=args.topic_id,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )

            sender_config = SenderConfig(
                chat_id=args.chat_id,
                topic_id=args.topic_id,
                group_size=args.group_size,
                send_interval=0,
                batch_delay=args.batch_delay,
                pause_every=0,
                pause_seconds=0,
                max_dimension=args.max_dimension,
                max_bytes=args.max_bytes,
                png_start_level=args.png_start_level,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )
            sent, skipped, sent_bytes = await drain_queue(
                sender_config,
                queue,
                url_pool,
                token_pool,
                zip_passwords=zip_passwords,
                queue_retries=queue_retries,
                progress=not args.no_progress,
            )

            finished_at = datetime.now()
            elapsed_seconds = time.monotonic() - started_clock
            avg_seconds = elapsed_seconds / sent if sent > 0 else 0.0
            await send_message(
                url_pool,
                token_pool,
                args.chat_id,
                "Completed image upload from %s at %s (elapsed %s, avg/image %s, total %s, avg %s, sent %d, skipped %d)"
                % (
                    args.queue_file,
                    _format_timestamp(finished_at),
                    _format_duration(elapsed_seconds),
                    _format_duration(avg_seconds),
                    _format_bytes(sent_bytes),
                    _format_speed(sent_bytes, elapsed_seconds),
                    sent,
                    skipped,
                ),
                topic_id=args.topic_id,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )
            _print_summary(
                "image",
                str(args.queue_file),
                started_at,
                finished_at,
                elapsed_seconds,
                sent,
                skipped,
                sent_bytes,
            )
            return

        for image_dir in image_dirs:
            if not image_dir.exists():
                raise SystemExit(f"Image directory not found: {image_dir}")
            await send_images_from_dir(
                url_pool,
                token_pool,
                args.chat_id,
                image_dir,
                topic_id=args.topic_id,
                group_size=args.group_size,
                start_index=args.start_index,
                end_index=args.end_index,
                batch_delay=args.batch_delay,
                progress=not args.no_progress,
                include_globs=include_globs,
                exclude_globs=exclude_globs,
                enable_zip=args.enable_zip,
                zip_passwords=zip_passwords,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )
        for zip_file in zip_files:
            if not zip_file.exists():
                raise SystemExit(f"Zip file not found: {zip_file}")
            await send_images_from_zip(
                url_pool,
                token_pool,
                args.chat_id,
                zip_file,
                topic_id=args.topic_id,
                group_size=args.group_size,
                start_index=args.start_index,
                end_index=args.end_index,
                batch_delay=args.batch_delay,
                progress=not args.no_progress,
                include_globs=include_globs,
                exclude_globs=exclude_globs,
                zip_passwords=zip_passwords,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )
        return

    if args.command in {"send-file", "send-video", "send-audio"}:
        file_paths = args.file or []
        zip_files = args.zip_file or []
        dir_paths = args.dir or []
        if not file_paths and not dir_paths and not zip_files:
            raise SystemExit("Provide --file, --dir, or --zip-file")
        send_type = {
            "send-file": "file",
            "send-video": "video",
            "send-audio": "audio",
        }[args.command]
        include_globs = _normalize_includes(args.include)
        exclude_globs = _normalize_excludes(args.exclude)
        zip_passwords = _load_zip_passwords(args.zip_pass, args.zip_pass_file)

        if args.queue_file:
            queue_retries = _validate_queue_retries(args.queue_retries)
            resolved_files = _resolve_paths(file_paths)
            resolved_dirs = _resolve_paths(dir_paths)
            resolved_zips = _resolve_paths(zip_files)
            meta = {
                "type": QUEUE_META_TYPE,
                "version": QUEUE_META_VERSION,
                "params": {
                    "command": args.command,
                    "chat_id": args.chat_id,
                    "topic_id": args.topic_id,
                    "files": [str(path) for path in resolved_files],
                    "dirs": [str(path) for path in resolved_dirs],
                    "zip_files": [str(path) for path in resolved_zips],
                    "include": include_globs,
                    "exclude": exclude_globs,
                    "start_index": args.start_index,
                    "end_index": args.end_index,
                    "batch_delay": args.batch_delay,
                    "enable_zip": args.enable_zip,
                    "queue_retries": queue_retries,
                    "max_retries": args.max_retries,
                    "retry_delay": args.retry_delay,
                },
            }
            try:
                queue = JsonlQueue(args.queue_file, meta=meta)
            except ValueError as exc:
                raise SystemExit(str(exc)) from exc

            for file_path in resolved_files:
                if not file_path.exists():
                    raise SystemExit(f"File not found: {file_path}")
                _enqueue_file_item(queue, file_path, send_type)
            for dir_path in resolved_dirs:
                if not dir_path.exists():
                    raise SystemExit(f"Directory not found: {dir_path}")
                _enqueue_files_from_dir(
                    queue,
                    dir_path,
                    send_type,
                    include_globs,
                    exclude_globs,
                    enable_zip=args.enable_zip,
                    start_index=args.start_index,
                    end_index=args.end_index,
                    zip_passwords=zip_passwords,
                )
            for zip_file in resolved_zips:
                if not zip_file.exists():
                    raise SystemExit(f"Zip file not found: {zip_file}")
                _enqueue_zip_files(
                    queue,
                    zip_file,
                    send_type,
                    include_globs,
                    exclude_globs,
                    start_index=args.start_index,
                    end_index=args.end_index,
                    zip_passwords=zip_passwords,
                )

            pending = queue.get_pending(max_attempts=queue_retries)
            if not pending:
                logging.info("No queued files to send.")
                return

            label = _send_type_label(send_type)
            started_at = datetime.now()
            started_clock = time.monotonic()
            await send_message(
                url_pool,
                token_pool,
                args.chat_id,
                f"Starting {label} upload from queue: {len(pending)} file(s) at {_format_timestamp(started_at)}",
                topic_id=args.topic_id,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )

            sender_config = SenderConfig(
                chat_id=args.chat_id,
                topic_id=args.topic_id,
                group_size=1,
                send_interval=0,
                batch_delay=args.batch_delay,
                pause_every=0,
                pause_seconds=0,
                max_dimension=0,
                max_bytes=0,
                png_start_level=0,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )
            sent, skipped, sent_bytes = await drain_queue(
                sender_config,
                queue,
                url_pool,
                token_pool,
                zip_passwords=zip_passwords,
                queue_retries=queue_retries,
                progress=not args.no_progress,
            )

            finished_at = datetime.now()
            elapsed_seconds = time.monotonic() - started_clock
            avg_seconds = elapsed_seconds / sent if sent > 0 else 0.0
            await send_message(
                url_pool,
                token_pool,
                args.chat_id,
                "Completed %s upload from %s at %s (elapsed %s, avg/%s %s, total %s, avg %s, sent %d, skipped %d)"
                % (
                    label,
                    args.queue_file,
                    _format_timestamp(finished_at),
                    _format_duration(elapsed_seconds),
                    label,
                    _format_duration(avg_seconds),
                    _format_bytes(sent_bytes),
                    _format_speed(sent_bytes, elapsed_seconds),
                    sent,
                    skipped,
                ),
                topic_id=args.topic_id,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )
            _print_summary(
                label,
                str(args.queue_file),
                started_at,
                finished_at,
                elapsed_seconds,
                sent,
                skipped,
                sent_bytes,
            )
            return

        for file_path in file_paths:
            if not file_path.exists():
                raise SystemExit(f"File not found: {file_path}")
            data = file_path.read_bytes()
            if send_type == "file":
                await send_document(
                    url_pool,
                    token_pool,
                    args.chat_id,
                    file_path.name,
                    data,
                    topic_id=args.topic_id,
                    max_retries=args.max_retries,
                    retry_delay=args.retry_delay,
                )
            elif send_type == "video":
                await send_video(
                    url_pool,
                    token_pool,
                    args.chat_id,
                    file_path.name,
                    data,
                    topic_id=args.topic_id,
                    max_retries=args.max_retries,
                    retry_delay=args.retry_delay,
                )
            else:
                await send_audio(
                    url_pool,
                    token_pool,
                    args.chat_id,
                    file_path.name,
                    data,
                    topic_id=args.topic_id,
                    max_retries=args.max_retries,
                    retry_delay=args.retry_delay,
                )
        for dir_path in dir_paths:
            if not dir_path.exists():
                raise SystemExit(f"Directory not found: {dir_path}")
            await send_files_from_dir(
                url_pool,
                token_pool,
                args.chat_id,
                dir_path,
                send_type=send_type,
                topic_id=args.topic_id,
                start_index=args.start_index,
                end_index=args.end_index,
                batch_delay=args.batch_delay,
                progress=not args.no_progress,
                include_globs=include_globs,
                exclude_globs=exclude_globs,
                enable_zip=args.enable_zip,
                zip_passwords=zip_passwords,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )
        for zip_file in zip_files:
            if not zip_file.exists():
                raise SystemExit(f"Zip file not found: {zip_file}")
            await send_files_from_zip(
                url_pool,
                token_pool,
                args.chat_id,
                zip_file,
                send_type=send_type,
                topic_id=args.topic_id,
                start_index=args.start_index,
                end_index=args.end_index,
                batch_delay=args.batch_delay,
                progress=not args.no_progress,
                include_globs=include_globs,
                exclude_globs=exclude_globs,
                zip_passwords=zip_passwords,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )
        return

    if args.command == "send-mixed":
        file_paths = args.file or []
        dir_paths = args.dir or []
        zip_files = args.zip_file or []
        if not file_paths and not dir_paths and not zip_files:
            raise SystemExit("Provide --file, --dir, or --zip-file")

        with_image = args.with_image
        with_video = args.with_video
        with_audio = args.with_audio
        with_file = args.with_file
        if not any([with_image, with_video, with_audio, with_file]):
            with_image = True
            with_video = True
            with_audio = True
            with_file = True

        zip_passwords = _load_zip_passwords(args.zip_pass, args.zip_pass_file)
        include_globs = _normalize_includes(args.include)
        exclude_globs = _normalize_excludes(args.exclude)

        if args.queue_file:
            queue_retries = _validate_queue_retries(args.queue_retries)
            resolved_files = _resolve_paths(file_paths)
            resolved_dirs = _resolve_paths(dir_paths)
            resolved_zips = _resolve_paths(zip_files)
            meta = {
                "type": QUEUE_META_TYPE,
                "version": QUEUE_META_VERSION,
                "params": {
                    "command": "send-mixed",
                    "chat_id": args.chat_id,
                    "topic_id": args.topic_id,
                    "files": [str(path) for path in resolved_files],
                    "dirs": [str(path) for path in resolved_dirs],
                    "zip_files": [str(path) for path in resolved_zips],
                    "include": include_globs,
                    "exclude": exclude_globs,
                    "group_size": args.group_size,
                    "batch_delay": args.batch_delay,
                    "enable_zip": args.enable_zip,
                    "with_image": with_image,
                    "with_video": with_video,
                    "with_audio": with_audio,
                    "with_file": with_file,
                    "queue_retries": queue_retries,
                    "max_retries": args.max_retries,
                    "retry_delay": args.retry_delay,
                    "max_dimension": args.max_dimension,
                    "max_bytes": args.max_bytes,
                    "png_start_level": args.png_start_level,
                },
            }
            try:
                queue = JsonlQueue(args.queue_file, meta=meta)
            except ValueError as exc:
                raise SystemExit(str(exc)) from exc

            if resolved_files:
                for file_path in resolved_files:
                    if not file_path.exists():
                        raise SystemExit(f"File not found: {file_path}")
                _enqueue_mixed_from_paths(
                    queue,
                    resolved_files,
                    include_globs,
                    exclude_globs,
                    with_image=with_image,
                    with_video=with_video,
                    with_audio=with_audio,
                    with_file=with_file,
                    enable_zip=args.enable_zip,
                    zip_passwords=zip_passwords,
                    apply_filters=True,
                )

            for dir_path in resolved_dirs:
                if not dir_path.exists():
                    raise SystemExit(f"Directory not found: {dir_path}")
                dir_files = _collect_source_files(
                    dir_path, include_globs, exclude_globs
                )
                _enqueue_mixed_from_paths(
                    queue,
                    dir_files,
                    include_globs,
                    exclude_globs,
                    with_image=with_image,
                    with_video=with_video,
                    with_audio=with_audio,
                    with_file=with_file,
                    enable_zip=args.enable_zip,
                    zip_passwords=zip_passwords,
                    apply_filters=False,
                )

            for zip_file in resolved_zips:
                if not zip_file.exists():
                    raise SystemExit(f"Zip file not found: {zip_file}")
                _enqueue_zip_mixed(
                    queue,
                    zip_file,
                    include_globs,
                    exclude_globs,
                    with_image=with_image,
                    with_video=with_video,
                    with_audio=with_audio,
                    with_file=with_file,
                    zip_passwords=zip_passwords,
                )

            pending = queue.get_pending(max_attempts=queue_retries)
            if not pending:
                logging.info("No queued items to send.")
                return

            started_at = datetime.now()
            started_clock = time.monotonic()
            await send_message(
                url_pool,
                token_pool,
                args.chat_id,
                f"Starting mixed upload from queue: {len(pending)} file(s) at {_format_timestamp(started_at)}",
                topic_id=args.topic_id,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )

            sender_config = SenderConfig(
                chat_id=args.chat_id,
                topic_id=args.topic_id,
                group_size=args.group_size,
                send_interval=0,
                batch_delay=args.batch_delay,
                pause_every=0,
                pause_seconds=0,
                max_dimension=args.max_dimension,
                max_bytes=args.max_bytes,
                png_start_level=args.png_start_level,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )
            sent, skipped, sent_bytes = await drain_queue(
                sender_config,
                queue,
                url_pool,
                token_pool,
                zip_passwords=zip_passwords,
                queue_retries=queue_retries,
                progress=not args.no_progress,
            )

            finished_at = datetime.now()
            elapsed_seconds = time.monotonic() - started_clock
            avg_seconds = elapsed_seconds / sent if sent > 0 else 0.0
            await send_message(
                url_pool,
                token_pool,
                args.chat_id,
                "Completed mixed upload from %s at %s (elapsed %s, avg/item %s, total %s, avg %s, sent %d, skipped %d)"
                % (
                    args.queue_file,
                    _format_timestamp(finished_at),
                    _format_duration(elapsed_seconds),
                    _format_duration(avg_seconds),
                    _format_bytes(sent_bytes),
                    _format_speed(sent_bytes, elapsed_seconds),
                    sent,
                    skipped,
                ),
                topic_id=args.topic_id,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )
            _print_summary(
                "mixed",
                str(args.queue_file),
                started_at,
                finished_at,
                elapsed_seconds,
                sent,
                skipped,
                sent_bytes,
            )
            return

        if file_paths:
            for file_path in file_paths:
                if not file_path.exists():
                    raise SystemExit(f"File not found: {file_path}")
            await send_mixed_from_paths(
                url_pool,
                token_pool,
                args.chat_id,
                file_paths,
                source_label="files",
                with_image=with_image,
                with_video=with_video,
                with_audio=with_audio,
                with_file=with_file,
                group_size=args.group_size,
                batch_delay=args.batch_delay,
                progress=not args.no_progress,
                include_globs=include_globs,
                exclude_globs=exclude_globs,
                enable_zip=args.enable_zip,
                zip_passwords=zip_passwords,
                topic_id=args.topic_id,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )

        for dir_path in dir_paths:
            if not dir_path.exists():
                raise SystemExit(f"Directory not found: {dir_path}")
            await send_mixed_from_dir(
                url_pool,
                token_pool,
                args.chat_id,
                dir_path,
                with_image=with_image,
                with_video=with_video,
                with_audio=with_audio,
                with_file=with_file,
                group_size=args.group_size,
                batch_delay=args.batch_delay,
                progress=not args.no_progress,
                include_globs=include_globs,
                exclude_globs=exclude_globs,
                enable_zip=args.enable_zip,
                zip_passwords=zip_passwords,
                topic_id=args.topic_id,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )

        for zip_file in zip_files:
            if not zip_file.exists():
                raise SystemExit(f"Zip file not found: {zip_file}")
            await send_mixed_from_zip(
                url_pool,
                token_pool,
                args.chat_id,
                zip_file,
                with_image=with_image,
                with_video=with_video,
                with_audio=with_audio,
                with_file=with_file,
                group_size=args.group_size,
                batch_delay=args.batch_delay,
                progress=not args.no_progress,
                include_globs=include_globs,
                exclude_globs=exclude_globs,
                zip_passwords=zip_passwords,
                topic_id=args.topic_id,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )
        return

    if args.command == "watch":
        watch_dirs = args.watch_dir or []
        if not watch_dirs:
            raise SystemExit("Provide --watch-dir")
        for watch_dir in watch_dirs:
            if not watch_dir.exists():
                raise SystemExit(f"Watch directory not found: {watch_dir}")

        with_image = args.with_image
        with_video = args.with_video
        with_audio = args.with_audio
        with_all = args.all
        if with_all:
            with_image = True
            with_video = True
            with_audio = True
        if not any([with_image, with_video, with_audio, with_all]):
            with_image = True
        include_globs = _normalize_includes(args.include)
        exclude_globs = _normalize_excludes(args.exclude)
        resolved_watch_dirs = [watch_dir.resolve() for watch_dir in watch_dirs]
        watch_dir_meta = (
            str(resolved_watch_dirs[0])
            if len(resolved_watch_dirs) == 1
            else [str(path) for path in resolved_watch_dirs]
        )
        meta = {
            "type": QUEUE_META_TYPE,
            "version": QUEUE_META_VERSION,
            "params": {
                "command": "watch",
                "watch_dir": watch_dir_meta,
                "recursive": args.recursive,
                "chat_id": args.chat_id,
                "topic_id": args.topic_id,
                "with_image": with_image,
                "with_video": with_video,
                "with_audio": with_audio,
                "with_all": with_all,
                "include": include_globs,
                "exclude": exclude_globs,
            },
        }
        try:
            queue = JsonlQueue(args.queue_file, meta=meta)
        except ValueError as exc:
            raise SystemExit(str(exc)) from exc
        watch_configs = [
            WatchConfig(
                root=watch_dir,
                recursive=args.recursive,
                exclude_globs=exclude_globs,
                include_globs=include_globs,
                with_image=with_image,
                with_video=with_video,
                with_audio=with_audio,
                with_all=with_all,
                scan_interval=args.scan_interval,
                settle_seconds=args.settle_seconds,
            )
            for watch_dir in resolved_watch_dirs
        ]
        sender_config = SenderConfig(
            chat_id=args.chat_id,
            topic_id=args.topic_id,
            group_size=args.group_size,
            send_interval=args.send_interval,
            batch_delay=args.batch_delay,
            pause_every=args.pause_every,
            pause_seconds=args.pause_seconds,
            max_dimension=args.max_dimension,
            max_bytes=args.max_bytes,
            png_start_level=args.png_start_level,
            max_retries=args.max_retries,
            retry_delay=args.retry_delay,
        )
        notify_config = NotifyConfig(
            enabled=args.notify,
            interval=args.notify_interval,
            notify_on_idle=True,
        )

        tasks = [watch_loop(config, queue) for config in watch_configs]
        tasks.append(
            sender_loop(
                sender_config,
                queue,
                url_pool,
                token_pool,
                zip_passwords=_load_zip_passwords(
                    args.zip_pass, args.zip_pass_file
                ),
            )
        )
        if notify_config.enabled:
            tasks.append(
                notify_loop(
                    notify_config,
                    queue,
                    url_pool,
                    token_pool,
                    args.chat_id,
                    args.topic_id,
                )
            )

        await asyncio.gather(*tasks)
        return

    raise SystemExit(f"Unknown command: {args.command}")


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()

    logging.basicConfig(
        level=getattr(logging, args.log_level.upper(), logging.INFO),
        format="%(asctime)s - %(levelname)s - %(message)s",
    )

    asyncio.run(run_command(args))


if __name__ == "__main__":
    main()
