from __future__ import annotations

import asyncio
import fnmatch
import logging
import os
import time
import zipfile
from dataclasses import dataclass
from pathlib import Path

from .constants import IMAGE_EXTENSIONS
from .queue import JsonlQueue, build_fingerprint, build_source_fingerprint


@dataclass
class WatchConfig:
    root: Path
    recursive: bool
    include_globs: list[str]
    exclude_globs: list[str]
    scan_interval: int
    settle_seconds: int


class StabilityTracker:
    def __init__(self, settle_seconds: int) -> None:
        self.settle_seconds = settle_seconds
        self.state: dict[Path, tuple[int, int, float]] = {}

    def is_stable(self, path: Path, size: int, mtime_ns: int) -> bool:
        now = time.monotonic()
        entry = self.state.get(path)
        if entry is None:
            self.state[path] = (size, mtime_ns, now)
            return False

        last_size, last_mtime, last_change = entry
        if size != last_size or mtime_ns != last_mtime:
            self.state[path] = (size, mtime_ns, now)
            return False

        if now - last_change >= self.settle_seconds:
            self.state.pop(path, None)
            return True
        return False

    def prune_missing(self, existing_paths: set[Path]) -> None:
        for path in list(self.state.keys()):
            if path not in existing_paths:
                self.state.pop(path, None)


def _matches_include(rel_path: str, patterns: list[str]) -> bool:
    if not patterns:
        return True
    for pattern in patterns:
        if not pattern:
            continue
        if fnmatch.fnmatch(rel_path, pattern):
            return True
    return False


def _matches_exclude(rel_path: str, patterns: list[str]) -> bool:
    for pattern in patterns:
        if not pattern:
            continue
        if fnmatch.fnmatch(rel_path, pattern):
            return True
    return False


def _iter_files(
    root: Path,
    recursive: bool,
    include_globs: list[str],
    exclude_globs: list[str],
) -> list[Path]:
    files: list[Path] = []
    if recursive:
        for base, dirs, filenames in os.walk(root):
            base_path = Path(base)
            rel_dir = str(base_path.relative_to(root))
            dirs[:] = [
                dirname
                for dirname in dirs
                if _matches_include(
                    str(Path(rel_dir) / dirname).lstrip("./"), include_globs
                )
                and not _matches_exclude(
                    str(Path(rel_dir) / dirname).lstrip("./"), exclude_globs
                )
            ]
            for filename in filenames:
                rel_file = str(Path(rel_dir) / filename).lstrip("./")
                if not _matches_include(rel_file, include_globs):
                    continue
                if _matches_exclude(rel_file, exclude_globs):
                    continue
                files.append(base_path / filename)
    else:
        for entry in root.iterdir():
            if entry.is_dir():
                continue
            rel_file = entry.name
            if not _matches_include(rel_file, include_globs):
                continue
            if _matches_exclude(rel_file, exclude_globs):
                continue
            files.append(entry)
    return files


def _is_candidate(path: Path) -> bool:
    lower_name = path.name.lower()
    if lower_name.endswith(".zip"):
        return True
    return lower_name.endswith(IMAGE_EXTENSIONS)


def _enqueue_file(
    queue: JsonlQueue, path: Path, size: int, mtime_ns: int
) -> None:
    source_path = str(path)
    source_fingerprint = build_source_fingerprint(source_path, size, mtime_ns)
    queue.enqueue_item(
        source_type="file",
        source_path=source_path,
        source_fingerprint=source_fingerprint,
        path=source_path,
        inner_path=None,
        size=size,
        mtime_ns=mtime_ns,
    )


def _enqueue_zip(
    queue: JsonlQueue,
    zip_path: Path,
    size: int,
    mtime_ns: int,
    include_globs: list[str],
    exclude_globs: list[str],
) -> int:
    source_path = str(zip_path)
    source_fingerprint = build_source_fingerprint(source_path, size, mtime_ns)
    if queue.has_source_fingerprint("zip", source_fingerprint):
        return 0

    added = 0
    try:
        with zipfile.ZipFile(zip_path, "r") as zip_ref:
            for info in zip_ref.infolist():
                if info.is_dir():
                    continue
                inner_path = info.filename
                if not _matches_include(inner_path, include_globs):
                    continue
                if _matches_exclude(inner_path, exclude_globs):
                    continue
                if not inner_path.lower().endswith(IMAGE_EXTENSIONS):
                    continue
                item = queue.enqueue_item(
                    source_type="zip",
                    source_path=source_path,
                    source_fingerprint=source_fingerprint,
                    path=source_path,
                    inner_path=inner_path,
                    size=info.file_size,
                    mtime_ns=None,
                    crc=info.CRC,
                )
                if item:
                    added += 1
    except zipfile.BadZipFile:
        logging.warning("Invalid zip file: %s", zip_path)
    return added


def scan_once(config: WatchConfig, queue: JsonlQueue, tracker: StabilityTracker) -> int:
    root = config.root
    candidates = _iter_files(
        root,
        config.recursive,
        config.include_globs,
        config.exclude_globs,
    )
    seen: set[Path] = set()
    enqueued = 0

    for path in candidates:
        seen.add(path)
        if not _is_candidate(path):
            continue

        try:
            stat = path.stat()
        except FileNotFoundError:
            continue

        fingerprint = build_fingerprint(
            "file", str(path), None, stat.st_size, stat.st_mtime_ns, None
        )
        if queue.has_fingerprint(fingerprint):
            continue

        if path.name.lower().endswith(".zip"):
            source_fingerprint = build_source_fingerprint(
                str(path), stat.st_size, stat.st_mtime_ns
            )
            if queue.has_source_fingerprint("zip", source_fingerprint):
                continue
            if not tracker.is_stable(path, stat.st_size, stat.st_mtime_ns):
                continue
            enqueued += _enqueue_zip(
                queue,
                path,
                stat.st_size,
                stat.st_mtime_ns,
                config.include_globs,
                config.exclude_globs,
            )
        else:
            if not tracker.is_stable(path, stat.st_size, stat.st_mtime_ns):
                continue
            before = len(queue.items)
            _enqueue_file(queue, path, stat.st_size, stat.st_mtime_ns)
            if len(queue.items) > before:
                enqueued += 1

    tracker.prune_missing(seen)
    return enqueued


async def watch_loop(config: WatchConfig, queue: JsonlQueue) -> None:
    tracker = StabilityTracker(config.settle_seconds)
    while True:
        enqueued = scan_once(config, queue, tracker)
        if enqueued:
            logging.info("Enqueued %d new file(s)", enqueued)
        await asyncio.sleep(config.scan_interval)
