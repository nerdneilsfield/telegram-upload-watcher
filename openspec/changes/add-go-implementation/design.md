## Context
We need a Go version that mirrors the Python watcher pipeline for easier deployment and parity.

## Goals / Non-Goals
- Goals: same CLI capabilities, same config format, same queue semantics, and image preprocessing defaults.
- Non-Goals: changing behavior or adding new features beyond parity.

## Decisions
- Place Go implementation under a dedicated directory (e.g., go/) with a cmd/ entrypoint.
- Use a high-performance HTTP client (e.g., fasthttp) for Telegram API calls and multipart uploads.
- Use a small INI parser (e.g., gopkg.in/ini.v1) to match existing config format.
- Use a lightweight image library (e.g., github.com/disintegration/imaging) for resize.
- Keep JSONL queue format compatible with the Python queue fields.
- Implement a high-performance queue with buffered channels and batched JSONL appends to avoid blocking the watcher.

## Risks / Trade-offs
- Image resizing/compression requires third-party libraries.
- Keeping parity across two implementations adds maintenance overhead.

## Migration Plan
- Add Go module without affecting the existing Python tool.
- Document separate usage and binary name to avoid conflicts.

## Open Questions
- Preferred Go version and module path?
- Preferred binary name (telegram-send-go vs another)?
- Should Go output directory be /go or /cmd at repo root?
- Do you want any web server component (Fiber), or only a high-performance HTTP client (fasthttp) for Telegram API?
