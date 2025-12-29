## Why
The Go CLI is powerful but requires manual flags/config edits. A desktop GUI will
make configuration safer, allow toggling features, and give live progress visibility
for long watch/send runs.

## What Changes
- Add a Wails-based desktop GUI for the Go implementation (Windows/Linux/macOS) with a Svelte UI.
- Provide a configuration editor for Telegram + watch/send settings.
- Show live progress metrics: current file, remaining file count, per-file time, ETA.
- Add start/pause/continue/stop controls for watch/send operations.
- Add mode tabs for watch and send commands (images, file, video, audio) with advanced settings collapsible.

## Impact
- Affected specs: new `go-wails-gui` capability.
- Affected code: new Go GUI app, integration with watcher/sender/queue.
