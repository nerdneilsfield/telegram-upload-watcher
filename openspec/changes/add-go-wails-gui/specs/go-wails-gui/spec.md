## ADDED Requirements

### Requirement: Desktop GUI
The system SHALL provide a Wails-based desktop GUI for the Go implementation on
Windows, Linux, and macOS.

#### Scenario: Launch GUI
- **WHEN** the user starts the GUI application
- **THEN** the app loads the last-used settings and shows start/pause/continue/stop controls

### Requirement: Run Controls
The GUI SHALL provide start, pause, continue, and stop controls for watch/send operations.

#### Scenario: Pause and resume
- **WHEN** the user pauses a running session
- **THEN** scanning/sending halts without losing the queue
- **WHEN** the user continues
- **THEN** scanning/sending resumes from the queue state

### Requirement: Mode Tabs
The GUI SHALL provide tabs for watch mode and one-off send modes (send-images,
send-file, send-video, send-audio).

#### Scenario: Switch to send-images
- **WHEN** the user selects the send-images tab
- **THEN** the GUI shows inputs for image directory/zip and send options

### Requirement: Collapsible Advanced Options
The GUI SHALL group advanced settings in collapsible sections to reduce clutter.

#### Scenario: Toggle advanced settings
- **WHEN** the user expands advanced settings
- **THEN** additional tuning options (intervals, batching, filters) are shown

### Requirement: Configuration Editor
The GUI SHALL allow editing Telegram config values and watch/send settings,
including: `api_url`, bot tokens, `chat_id`, `topic_id`, watch directory,
include/exclude globs, send-type toggles, zip passwords, intervals, queue file,
and notify options.

#### Scenario: Save configuration
- **WHEN** the user updates settings and clicks save
- **THEN** the GUI persists INI config and GUI settings without exposing tokens in logs

### Requirement: Progress & Stats
The GUI SHALL display progress information for watch/send runs, including
current file, remaining file count, per-file time, and remaining time (ETA).

#### Scenario: Active sending
- **WHEN** files are being sent
- **THEN** the progress panel updates with current file, remaining count, per-file time, and ETA
