## ADDED Requirements
### Requirement: Polling Watcher
The system SHALL scan a target folder on a configurable interval (default 30s) and detect new files to enqueue.

#### Scenario: Periodic scan
- **WHEN** the watcher is running with the default interval
- **THEN** it scans the target folder every 30 seconds for new files

### Requirement: Optional Recursion
The system SHALL support non-recursive scans by default and enable recursion via a CLI flag.

#### Scenario: Non-recursive scan
- **WHEN** the watcher runs without --recursive
- **THEN** only the top-level folder is scanned

#### Scenario: Recursive scan
- **WHEN** the watcher runs with --recursive
- **THEN** subfolders are scanned for new files

### Requirement: Exclude Globs
The system SHALL accept glob patterns to exclude files or folders from scanning.

#### Scenario: Excluding paths
- **WHEN** exclude globs are configured
- **THEN** matching files are ignored during scanning

### Requirement: File Stability Window
The system SHALL wait for a configurable settle window (default 5s) before enqueueing a newly detected file.

#### Scenario: Settled file
- **WHEN** a new file stops changing for the settle window
- **THEN** the file is eligible for enqueue

### Requirement: JSONL Queue Persistence
The system SHALL persist queued file paths and statuses in a JSONL file so work can resume after restart.

#### Scenario: Restart recovery
- **WHEN** the process restarts
- **THEN** pending queue items are loaded from the JSONL file

### Requirement: Scheduled Sender
The system SHALL run a sender loop that dequeues items on a configurable interval and sends them to Telegram.

#### Scenario: Queue drain
- **WHEN** queued items are available
- **THEN** the sender processes and updates their statuses on each interval

### Requirement: Queue Status Tracking
The system SHALL record per-item status (queued, sending, sent, failed) for file entries in the queue for restart safety.

#### Scenario: Restart with mixed statuses
- **WHEN** a restart occurs with queued and sent items recorded
- **THEN** only pending items are re-processed and sent items are skipped

### Requirement: Expand Directory and Zip Inputs
The system SHALL expand directory and zip inputs into file-level queue entries before sending.

#### Scenario: Directory or zip input
- **WHEN** a directory or zip is added
- **THEN** its image files are expanded into file queue entries

### Requirement: Media Group Uploads
The system SHALL send image files using sendMediaGroup.

#### Scenario: Image upload
- **WHEN** image files are dequeued
- **THEN** they are batched and sent using sendMediaGroup

### Requirement: Image Dimension Limit
The system SHALL enforce a configurable maximum image dimension (default 2000px) by scaling oversized images proportionally before upload.

#### Scenario: Image is too large
- **WHEN** an image exceeds the configured max dimension
- **THEN** it is resized proportionally to fit within the limit

### Requirement: Image Size Compression
The system SHALL compress oversized images to PNG when they exceed a configurable size threshold (default 5MB), using a greedy compression-level search starting at level 8.

#### Scenario: Image file size exceeds threshold
- **WHEN** an image exceeds the configured size threshold
- **THEN** it is recompressed to PNG before upload

#### Scenario: Greedy compression
- **WHEN** PNG compression is required
- **THEN** the system adjusts compression level from a starting level of 8 to reach the size threshold

### Requirement: Send Pause
The system SHALL support a configurable pause after sending a set number of images.

#### Scenario: Pause after batch
- **WHEN** the sender reaches the configured image count
- **THEN** it pauses sending for the configured duration before continuing

### Requirement: Default Config Example
The system SHALL provide a default INI config example for Telegram setup.

#### Scenario: Example config is available
- **WHEN** a user checks the repository
- **THEN** a sample config file shows the required fields and token format
