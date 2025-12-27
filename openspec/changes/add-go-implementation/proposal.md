## Why
We need a Go implementation with the same features as the Python watcher so we can deploy a single static binary and reuse the same workflows.

## What Changes
- Add a Go module in the repository with a CLI that mirrors the Python commands.
- Implement polling watch, JSONL queue persistence, sender loop, and notifications.
- Implement image preprocessing (resize + PNG compression) and Telegram uploads.

## Impact
- Affected specs: go-watcher-sender (new)
- Affected code: new Go module, CLI, and documentation updates
