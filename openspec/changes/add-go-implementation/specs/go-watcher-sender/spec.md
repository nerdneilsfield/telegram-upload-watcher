## ADDED Requirements
### Requirement: Go CLI Parity
The system SHALL provide a Go CLI that mirrors the Python commands: send-message, send-images, and watch.

#### Scenario: CLI availability
- **WHEN** the Go binary is installed
- **THEN** the same command set is available

### Requirement: INI Configuration
The system SHALL load Telegram configuration from an INI file with [Telegram] api_url and [Token*] sections.

#### Scenario: Config file provided
- **WHEN** a config file is passed
- **THEN** api_url and token values are loaded

### Requirement: Polling Watcher
The system SHALL scan a target folder on a configurable interval (default 30s) with optional recursion and glob excludes.

#### Scenario: Recursive scan
- **WHEN** --recursive is enabled
- **THEN** subfolders are scanned for new files

### Requirement: File Stability Window
The system SHALL wait for a configurable settle window (default 5s) before enqueueing a newly detected file.

#### Scenario: Settled file
- **WHEN** a file stops changing for the settle window
- **THEN** it becomes eligible for enqueue

### Requirement: JSONL Queue Persistence
The system SHALL persist queued file paths and statuses in a JSONL file so work can resume after restart.

#### Scenario: Restart recovery
- **WHEN** the process restarts
- **THEN** pending queue items are loaded

### Requirement: High-Performance Queue
The system SHALL use a buffered queue design so watcher scans are not blocked by queue persistence I/O.

#### Scenario: High enqueue rate
- **WHEN** many files are detected in a short period
- **THEN** the watcher can enqueue without blocking on disk writes

### Requirement: Expand Directory and Zip Inputs
The system SHALL expand directory and zip inputs into file-level queue entries before sending.

#### Scenario: Directory or zip input
- **WHEN** a directory or zip is added
- **THEN** its image files are expanded into file queue entries

### Requirement: Image Preprocessing Defaults
The system SHALL enforce a configurable maximum image dimension (default 2000px) and size threshold (default 5MB), with greedy PNG compression starting at level 8.

#### Scenario: Oversized image
- **WHEN** an image exceeds the configured dimension or size
- **THEN** it is resized and/or recompressed before upload

### Requirement: Telegram Uploads with Topic
The system SHALL send text via sendMessage and images via sendMediaGroup, supporting optional topic_id.

#### Scenario: Send to topic
- **WHEN** a topic_id is provided
- **THEN** messages are sent to that thread

### Requirement: Sender Pause Controls
The system SHALL support a configurable pause after sending N images.

#### Scenario: Pause after batch
- **WHEN** the sender reaches the configured image count
- **THEN** it pauses for the configured duration

### Requirement: Watch Notifications
The system SHALL optionally send start/status/idle notifications with elapsed time.

#### Scenario: Status updates
- **WHEN** notifications are enabled
- **THEN** status messages include elapsed time and queue counts
