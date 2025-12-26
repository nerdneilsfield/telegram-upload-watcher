from __future__ import annotations

import json
import logging
import uuid
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


STATUS_QUEUED = "queued"
STATUS_SENDING = "sending"
STATUS_SENT = "sent"
STATUS_FAILED = "failed"

PENDING_STATUSES = {STATUS_QUEUED, STATUS_FAILED}


@dataclass
class QueueItem:
    id: str
    source_type: str
    source_path: str
    source_fingerprint: str
    path: str
    inner_path: str | None
    size: int
    mtime_ns: int | None
    crc: int | None
    fingerprint: str
    status: str
    enqueued_at: str
    updated_at: str
    error: str | None = None


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat()


def build_fingerprint(
    source_type: str,
    path: str,
    inner_path: str | None,
    size: int,
    mtime_ns: int | None,
    crc: int | None,
) -> str:
    parts = [source_type, path]
    if inner_path:
        parts.append(inner_path)
    parts.append(str(size))
    if mtime_ns is not None:
        parts.append(str(mtime_ns))
    if crc is not None:
        parts.append(str(crc))
    return "|".join(parts)


def build_source_fingerprint(
    source_path: str, size: int, mtime_ns: int | None
) -> str:
    parts = [source_path, str(size)]
    if mtime_ns is not None:
        parts.append(str(mtime_ns))
    return "|".join(parts)


class JsonlQueue:
    def __init__(self, path: Path) -> None:
        self.path = path
        self.items: dict[str, QueueItem] = {}
        self.fingerprint_index: dict[str, str] = {}
        self.source_index: set[str] = set()
        self._load()

    def _load(self) -> None:
        if not self.path.exists():
            return
        try:
            with self.path.open("r", encoding="utf-8") as handle:
                for line in handle:
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        payload = json.loads(line)
                    except json.JSONDecodeError:
                        logging.warning("Skipping invalid queue line")
                        continue
                    item_id = payload.get("id")
                    if not item_id:
                        continue
                    try:
                        item = QueueItem(**payload)
                    except TypeError:
                        logging.warning("Skipping incompatible queue entry")
                        continue
                    self.items[item_id] = item
        except FileNotFoundError:
            return

        self._rebuild_indexes()

    def _rebuild_indexes(self) -> None:
        self.fingerprint_index.clear()
        self.source_index.clear()
        for item_id, item in self.items.items():
            self.fingerprint_index[item.fingerprint] = item_id
            source_key = f"{item.source_type}:{item.source_fingerprint}"
            self.source_index.add(source_key)

    def _append(self, item: QueueItem) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with self.path.open("a", encoding="utf-8") as handle:
            handle.write(json.dumps(item.__dict__, ensure_ascii=True) + "\n")

    def has_fingerprint(self, fingerprint: str) -> bool:
        return fingerprint in self.fingerprint_index

    def has_source_fingerprint(self, source_type: str, source_fingerprint: str) -> bool:
        return f"{source_type}:{source_fingerprint}" in self.source_index

    def enqueue_item(
        self,
        *,
        source_type: str,
        source_path: str,
        source_fingerprint: str,
        path: str,
        inner_path: str | None,
        size: int,
        mtime_ns: int | None,
        crc: int | None = None,
    ) -> QueueItem | None:
        fingerprint = build_fingerprint(
            source_type, path, inner_path, size, mtime_ns, crc
        )
        if fingerprint in self.fingerprint_index:
            return None

        now = _utc_now()
        item = QueueItem(
            id=uuid.uuid4().hex,
            source_type=source_type,
            source_path=source_path,
            source_fingerprint=source_fingerprint,
            path=path,
            inner_path=inner_path,
            size=size,
            mtime_ns=mtime_ns,
            crc=crc,
            fingerprint=fingerprint,
            status=STATUS_QUEUED,
            enqueued_at=now,
            updated_at=now,
            error=None,
        )
        self.items[item.id] = item
        self.fingerprint_index[item.fingerprint] = item.id
        self.source_index.add(f"{source_type}:{source_fingerprint}")
        self._append(item)
        return item

    def update_status(self, item_id: str, status: str, error: str | None = None) -> None:
        item = self.items.get(item_id)
        if not item:
            raise KeyError(f"Queue item not found: {item_id}")
        item.status = status
        item.updated_at = _utc_now()
        item.error = error
        self._append(item)

    def get_pending(self, limit: int | None = None) -> list[QueueItem]:
        pending = [
            item
            for item in self.items.values()
            if item.status in PENDING_STATUSES
        ]
        pending.sort(key=lambda entry: entry.enqueued_at)
        if limit is not None:
            return pending[:limit]
        return pending

    def stats(self) -> dict[str, Any]:
        counts = {
            STATUS_QUEUED: 0,
            STATUS_SENDING: 0,
            STATUS_SENT: 0,
            STATUS_FAILED: 0,
        }
        for item in self.items.values():
            if item.status in counts:
                counts[item.status] += 1
        return counts
