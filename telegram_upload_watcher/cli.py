import argparse
import asyncio
import logging
from pathlib import Path

from .config import load_config
from .pools import TokenPool, UrlPool
from .queue import JsonlQueue
from .sender import SenderConfig, sender_loop
from .telegram import (
    send_images_from_dir,
    send_images_from_zip,
    send_message,
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
    send_images_parser.add_argument("--image-dir", type=Path, help="Image directory")
    send_images_parser.add_argument("--zip-file", type=Path, help="Zip file path")
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

    watch_parser = subparsers.add_parser(
        "watch", parents=[common], help="Watch folder and send queued images"
    )
    watch_parser.add_argument(
        "--watch-dir", type=Path, required=True, help="Folder to watch"
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
        "--exclude",
        action="append",
        default=[],
        help="Glob pattern to exclude (repeatable or comma-separated)",
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
        if not args.image_dir and not args.zip_file:
            raise SystemExit("Provide --image-dir or --zip-file")
        if args.image_dir:
            if not args.image_dir.exists():
                raise SystemExit(f"Image directory not found: {args.image_dir}")
            await send_images_from_dir(
                url_pool,
                token_pool,
                args.chat_id,
                args.image_dir,
                topic_id=args.topic_id,
                group_size=args.group_size,
                start_index=args.start_index,
                end_index=args.end_index,
                batch_delay=args.batch_delay,
                progress=not args.no_progress,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )
        if args.zip_file:
            if not args.zip_file.exists():
                raise SystemExit(f"Zip file not found: {args.zip_file}")
            await send_images_from_zip(
                url_pool,
                token_pool,
                args.chat_id,
                args.zip_file,
                topic_id=args.topic_id,
                group_size=args.group_size,
                start_index=args.start_index,
                end_index=args.end_index,
                batch_delay=args.batch_delay,
                progress=not args.no_progress,
                max_retries=args.max_retries,
                retry_delay=args.retry_delay,
            )
        return

    if args.command == "watch":
        if not args.watch_dir.exists():
            raise SystemExit(f"Watch directory not found: {args.watch_dir}")

        queue = JsonlQueue(args.queue_file)
        watch_config = WatchConfig(
            root=args.watch_dir,
            recursive=args.recursive,
            exclude_globs=_normalize_excludes(args.exclude),
            scan_interval=args.scan_interval,
            settle_seconds=args.settle_seconds,
        )
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

        await asyncio.gather(
            watch_loop(watch_config, queue),
            sender_loop(sender_config, queue, url_pool, token_pool),
        )
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
