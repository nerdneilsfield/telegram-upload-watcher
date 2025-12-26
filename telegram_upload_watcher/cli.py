import argparse
import asyncio
import logging
from pathlib import Path

from .config import load_config
from .pools import TokenPool, UrlPool
from .telegram import (
    send_images_from_dir,
    send_images_from_zip,
    send_message,
    test_token,
)


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
