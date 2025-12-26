## Context
The project needs a repeatable Python environment setup and a reusable Telegram sending module before the folder watcher pipeline can be built.

## Goals / Non-Goals
- Goals: uv-based setup, simple Telegram client, configuration-driven tokens/URLs, and a CLI for basic sends.
- Non-Goals: full watcher implementation, database persistence, or advanced scheduling.

## Decisions
- Use uv for environment setup to standardize dependencies.
- Use direct Telegram Bot API calls (requests/aiohttp) rather than a heavyweight framework.
- Keep configuration in INI format with [Telegram] and [Token*] sections.
- Provide optional proxy support via HTTPS_PROXY/https_proxy environment variables.

## Risks / Trade-offs
- Direct API handling requires manual retry and rate-limit handling.
- Multiple tokens/URLs add complexity but improve reliability.

## Migration Plan
- Add uv files and new modules without breaking existing scripts.
- Provide a CLI that mirrors the reference behavior for easier validation.

## Open Questions
- Do we want a single entrypoint script or a package layout (src/)?
- Should media uploads support non-image files at this stage?
