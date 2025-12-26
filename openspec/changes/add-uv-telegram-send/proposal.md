## Why
We need a consistent Python environment setup and a baseline Telegram sending module so the watcher can enqueue folders and send their contents reliably.

## What Changes
- Add uv-based environment bootstrap files (pyproject.toml and uv.lock) with runtime dependencies for Telegram sending.
- Implement a minimal Telegram sending framework based on the reference scripts (message + media group).
- Provide configuration loading, URL/token pooling, retry, and proxy handling patterns.

## Impact
- Affected specs: telegram-send (new)
- Affected code: new core modules, CLI entrypoint, and documentation updates
