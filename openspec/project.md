# Project Context

## Purpose
This project monitors a root folder recursively. When a new subfolder appears, it is
enqueued for upload and its contents are sent to a Telegram channel/group/user. The
goal is reliable, ordered delivery with clear logging and minimal manual steps.

## Tech Stack
- Python 3.x
- Standard library: pathlib, asyncio/threading, logging, queue, configparser
- Filesystem monitoring: watchdog (preferred) or periodic scan (fallback)
- Telegram Bot API over HTTPS via direct HTTP calls (requests + aiohttp)
- Tooling helpers: tqdm for progress output

## Project Conventions

### Code Style
- PEP 8 naming and layout; snake_case for functions/variables, CapWords for classes
- Type hints on public functions where practical
- Keep modules small and single-purpose; avoid heavy frameworks

### Architecture Patterns
- Watcher -> queue -> uploader pipeline
- Separate modules for config, watcher, queue/persistence, and Telegram client
- URL pool + token pool for distributing load across multiple API endpoints/tokens
- Retry wrapper around Telegram API calls with backoff delays
- INI-based configuration with [Telegram] api_url and [Token*] sections
- Use idempotent processing to avoid duplicate uploads on restart

### Testing Strategy
- Unit tests for queueing, path filtering, and upload scheduling (pytest)
- Manual/integration tests for real Telegram uploads in a dev chat
- Log-based verification for watcher events

### Git Workflow
- main branch with short-lived feature branches
- Small, reviewable commits with descriptive messages
- No mandatory conventional-commit format unless added later

## Domain Context
- Telegram bots must be invited to channels/groups and need proper permissions
- Telegram has file size limits and rate limits; large folders may need throttling or batching
- sendMessage and sendMediaGroup are the primary endpoints; topic_id may be used
- Directory uploads are sent file-by-file (or zipped if configured)

## Important Constraints
- Must watch recursively and only enqueue newly created subfolders
- Avoid sending partially written files; allow a short "settle" window if needed
- Handle restarts without re-sending already uploaded folders (persisted state or markers)
- Respect proxy settings via HTTPS_PROXY/https_proxy when configured

## External Dependencies
- Telegram Bot API (bot token, chat ID)
- Filesystem notification backend (inotify/FSEvents) if using watchdog
- requests, aiohttp, tqdm
