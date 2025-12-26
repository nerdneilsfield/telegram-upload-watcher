import configparser
import logging
from pathlib import Path

from .pools import normalize_api_url


def load_config(config_path: Path) -> tuple[list[str], list[str]]:
    config = configparser.ConfigParser()
    config.read(config_path)

    api_url = config.get("Telegram", "api_url", fallback="https://api.telegram.org")
    api_urls = [
        normalize_api_url(url) for url in api_url.split(",") if url.strip()
    ]

    tokens: list[str] = []
    for section in config.sections():
        if not section.startswith("Token"):
            continue
        token = config.get(section, "token", fallback="").strip()
        if token:
            tokens.append(token)

    logging.info("Loaded %d api_url(s) and %d token(s)", len(api_urls), len(tokens))
    return api_urls, tokens
