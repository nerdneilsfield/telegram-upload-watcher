## Context
We will add a polling-based watcher that enqueues new files and a sender that drains a persistent queue. This is a new pipeline stage and introduces persistence concerns.

## Goals / Non-Goals
- Goals: periodic scanning (default 30s), optional recursion, glob excludes, JSONL queue persistence, settle-window protection, and scheduled sending.
- Non-Goals: real-time OS event watcher, database persistence, or non-image upload types.

## Decisions
- Use polling scans by default to avoid OS-specific watch complexity.
- JSONL queue with append-only entries and status updates for restart safety.
- Default settle window 5s before enqueue to avoid partial files.
- Use sendMediaGroup for image uploads; non-image files are skipped or logged.
- Preprocess images to enforce max dimension and file size constraints.
- Queue entries are file-level items; directory/zip inputs are expanded into per-file entries with source metadata.
- Default max dimension is 2000px and size threshold is 5MB; both are configurable.
- PNG compression uses a greedy search over compression level (start at level 8, adjust until size <= threshold).

## Risks / Trade-offs
- Polling may miss rapid create/delete cycles; settle window adds latency.
- JSONL queue needs compaction or pruning strategy over time.

## Migration Plan
- Add new modules and CLI flags without affecting existing send-only flows.
- Provide documentation for enabling watch mode.

## Open Questions
- Should we add queue compaction/rotation in the first version?
- Should we support non-image uploads in a follow-up change?
