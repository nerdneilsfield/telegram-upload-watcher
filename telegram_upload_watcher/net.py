import logging
import os


def get_proxy_from_env() -> str | None:
    https_proxy = os.environ.get("https_proxy") or os.environ.get("HTTPS_PROXY")
    if https_proxy:
        logging.info("Using HTTPS proxy: %s", https_proxy)
        return https_proxy
    return None
