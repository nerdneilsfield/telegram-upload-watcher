from __future__ import annotations

import asyncio
import time
from dataclasses import dataclass

from .queue import JsonlQueue
from .telegram import send_message
from .pools import TokenPool, UrlPool


@dataclass
class NotifyConfig:
    enabled: bool
    interval: int
    notify_on_idle: bool


def _format_elapsed(seconds: float) -> str:
    total = int(seconds)
    hours = total // 3600
    minutes = (total % 3600) // 60
    secs = total % 60
    return f\"{hours:02d}:{minutes:02d}:{secs:02d}\"


async def notify_loop(
    config: NotifyConfig,
    queue: JsonlQueue,
    url_pool: UrlPool,
    token_pool: TokenPool,
    chat_id: str,
    topic_id: int | None,
) -> None:
    if not config.enabled:
        return

    start = time.monotonic()
    last_pending = None

    await send_message(
        url_pool,
        token_pool,
        chat_id,
        f\"Watch started (elapsed {_format_elapsed(0)})\",
        topic_id=topic_id,
    )

    while True:
        await asyncio.sleep(max(1, config.interval))
        elapsed = _format_elapsed(time.monotonic() - start)
        stats = queue.stats()
        pending = stats.get(\"queued\", 0) + stats.get(\"failed\", 0)

        await send_message(
            url_pool,
            token_pool,
            chat_id,
            \"Watch status: elapsed {elapsed}, queued {queued}, sending {sending}, sent {sent}, failed {failed}\".format(
                elapsed=elapsed,
                queued=stats.get(\"queued\", 0),
                sending=stats.get(\"sending\", 0),
                sent=stats.get(\"sent\", 0),
                failed=stats.get(\"failed\", 0),
            ),
            topic_id=topic_id,
        )

        if config.notify_on_idle:
            if last_pending is None:
                last_pending = pending
            elif last_pending > 0 and pending == 0:
                await send_message(
                    url_pool,
                    token_pool,
                    chat_id,
                    f\"Watch idle (elapsed {elapsed})\",
                    topic_id=topic_id,
                )
            last_pending = pending
