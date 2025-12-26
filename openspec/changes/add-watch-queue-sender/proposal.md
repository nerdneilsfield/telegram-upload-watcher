## Why
We need a reliable watch pipeline that periodically scans folders, enqueues new files, and sends them on a schedule to Telegram.

## What Changes
- Add a polling-based watcher with optional recursion and glob-based excludes.
- Persist a JSONL queue of file paths and statuses for restart safety.
- Add a sender loop that drains the queue at a configured interval using sendMediaGroup for images.
- Add image preprocessing to enforce max dimension and size limits (scale and compress).
- Track queued/sent status for file entries; directories and zip files are expanded into file items for restart safety.
- Add a configurable pause after sending a batch of images to throttle uploads.
- Provide a default INI config example for setup.
- Add optional watch notifications (start/status/idle) with elapsed time.

## Impact
- Affected specs: watch-queue-sender (new)
- Affected code: new watcher/queue/sender modules and CLI flags
