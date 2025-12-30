import argparse
import asyncio
import logging
from pathlib import Path

from .config import load_config
from .pools import TokenPool, UrlPool
from .queue import JsonlQueue, QUEUE_META_TYPE, QUEUE_META_VERSION
from .notify import NotifyConfig, notify_loop
from .sender import SenderConfig, sender_loop
from .telegram import (
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
                include_globs=_normalize_includes(args.include),
                exclude_globs=_normalize_excludes(args.exclude),
                enable_zip=args.enable_zip,
                zip_passwords=_load_zip_passwords(args.zip_pass, args.zip_pass_file),
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
                include_globs=_normalize_includes(args.include),
                exclude_globs=_normalize_excludes(args.exclude),
                zip_passwords=_load_zip_passwords(args.zip_pass, args.zip_pass_file),
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
        zip_passwords = _load_zip_passwords(args.zip_pass, args.zip_pass_file)
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
                include_globs=_normalize_includes(args.include),
                exclude_globs=_normalize_excludes(args.exclude),
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
                include_globs=_normalize_includes(args.include),
                exclude_globs=_normalize_excludes(args.exclude),
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
