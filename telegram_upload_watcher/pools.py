import logging
import random


def normalize_api_url(url: str) -> str:
    url = url.strip()
    if not url:
        return ""
    if not url.startswith("https://"):
        url = f"https://{url}"
    return url.rstrip("/")


class UrlPool:
    def __init__(self, urls: list[str]) -> None:
        self.urls = [normalize_api_url(url) for url in urls if url.strip()]
        self.working_urls = [{"url": url, "count": 0} for url in self.urls]
        logging.info("Initialized API URL pool with %d URL(s)", len(self.urls))

    def get_url(self) -> str | None:
        if not self.working_urls:
            return None
        min_count = min(url["count"] for url in self.working_urls)
        candidates = [
            url for url in self.working_urls if url["count"] == min_count
        ]
        return random.choice(candidates)["url"]

    def increment_url(self, url_str: str | None) -> None:
        if not url_str:
            return
        for url in self.working_urls:
            if url["url"] == url_str:
                url["count"] += 1
                break

    def remove_url(self, url_str: str) -> None:
        self.working_urls = [
            url for url in self.working_urls if url["url"] != url_str
        ]
        logging.info("Removed API URL %s, remaining %d", url_str, len(self.working_urls))


class TokenPool:
    def __init__(self, tokens: list[str]) -> None:
        self.tokens = [token.strip() for token in tokens if token.strip()]
        self.working_tokens = [
            {"token": token, "count": 0} for token in self.tokens
        ]
        logging.info("Initialized token pool with %d token(s)", len(self.tokens))

    def get_token(self) -> str | None:
        if not self.working_tokens:
            return None
        min_count = min(token["count"] for token in self.working_tokens)
        candidates = [
            token for token in self.working_tokens if token["count"] == min_count
        ]
        return random.choice(candidates)["token"]

    def increment_token(self, token_str: str | None) -> None:
        if not token_str:
            return
        for token in self.working_tokens:
            if token["token"] == token_str:
                token["count"] += 1
                break

    def remove_token(self, token_str: str) -> None:
        self.working_tokens = [
            token for token in self.working_tokens if token["token"] != token_str
        ]
        logging.info(
            "Removed token %s, remaining %d", token_str, len(self.working_tokens)
        )
