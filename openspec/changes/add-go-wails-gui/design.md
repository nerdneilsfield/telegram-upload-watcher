## Overview
Implement a Wails-based desktop GUI for the Go implementation. The GUI owns a
runtime controller that starts/stops watch + sender loops and emits progress
events to the frontend.

## Architecture
- **Backend (Go/Wails):**
- `App` exposes methods: load/save config, start/pause/continue/stop watch, read stats.
- Use `context.Context` for cancellation; add a pause gate to suspend scanning/sending.
  - Translate queue stats + sender timing into progress payloads.
- **Frontend (Wails web UI):**
  - Svelte + Skeleton UI (Tailwind) for layout and controls.
  - Config editor form with validation.
  - Start/pause/continue/stop controls and status indicators.
  - Tabs for watch vs send actions; advanced settings in collapsible panels.
  - Progress panel showing current file, remaining count, per-file time, ETA.

## Config Handling
- Primary INI remains the Telegram config (`api_url`, token sections).
- GUI stores watch/send settings (watch dir, include/exclude, toggles, intervals,
  queue file, notify, send types) in a GUI settings file (e.g., JSON).
- GUI writes INI + GUI settings explicitly on save; never writes bot tokens to logs.

## Progress Model
- Track enqueue/send counts from queue stats.
- Track per-file send timing in sender loop; compute ETA from remaining count and
  recent per-file averages.
- Emit Wails events with a typed payload (`current_file`, `remaining_files`,
  `per_file_ms`, `eta_ms`, `status`).
