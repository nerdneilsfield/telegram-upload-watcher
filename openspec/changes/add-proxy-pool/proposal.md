## Why
CLI runs need optional, resilient proxy routing to Telegram when direct access is unreliable or blocked. A proxy pool with health checks reduces failed uploads and allows automatic recovery.

## What Changes
- Add `--proxy-list` for CLI commands (Python + Go) to load a list of proxies (http/socks5/socks5h) with optional credentials.
- Build a proxy pool that validates proxies via `getMe` and uses per-proxy HTTP clients for sending requests.
- Track proxy failures, temporarily remove unhealthy proxies, and periodically re-check to restore them.
- When `--proxy-list` is absent, keep existing behavior (env proxy if present, else direct).

## Impact
- Affected specs: proxy-pool (new)
- Affected code: Python Telegram client/pools + Go Telegram client/pools, CLI flag parsing, README examples
