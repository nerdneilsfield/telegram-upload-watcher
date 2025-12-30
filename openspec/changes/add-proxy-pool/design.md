## Context
CLI users want optional proxy routing via a list of HTTP/SOCKS5 proxies. The pool must validate connectivity to Telegram and handle transient failures without manual intervention.

## Goals / Non-Goals
- Goals:
  - Support `--proxy-list` for CLI in Python and Go.
  - Validate proxies via `getMe` before use.
  - Use per-proxy clients and rotate requests across active proxies.
  - Remove failing proxies and restore them via periodic re-checks.
- Non-Goals:
  - GUI integration (CLI only for now).
  - Per-request proxy overrides beyond the pool.

## Decisions
- Decision: Use a proxy pool with active/inactive sets and a background health-check loop.
  - Why: keeps request path simple while enabling recovery.
- Decision: Health check uses `getMe` with current token(s).
  - Why: cheap and validates Telegram connectivity.
- Decision: Support proxy strings with scheme and credentials, including the custom `user@pass:host:port` form and `socks5h` for remote DNS.

## Defaults
- `--proxy-failures`: 3 consecutive failures before removal.
- `--proxy-check-interval`: 60 seconds between background rechecks.

## Risks / Trade-offs
- Pool failures can stall if all proxies are unhealthy; CLI should surface clear errors.
- Background checks add request overhead; interval is configurable.

## Open Questions
- Confirm defaults for failure threshold and check interval.
- Should we allow comments in proxy list (`#` or `;`)?
