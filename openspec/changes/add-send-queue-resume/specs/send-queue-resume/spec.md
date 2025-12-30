## ADDED Requirements
### Requirement: Queue-backed Send Mode
The system SHALL support a queue-backed mode for send-images, send-file, send-video, send-audio, and send-mixed.

#### Scenario: Queue-enabled send
- **WHEN** a user passes `--queue-file`
- **THEN** items are enqueued to a JSONL file and processed in order

### Requirement: Resume from Queue
The system SHALL resume pending or failed items from an existing queue file.

#### Scenario: Resume after interruption
- **WHEN** a run is restarted with the same `--queue-file`
- **THEN** previously queued items with pending/failed status are retried

### Requirement: Queue Metadata Validation
Queue metadata SHALL record the command parameters and be validated on resume.

#### Scenario: Mismatched parameters
- **WHEN** the queue metadata does not match the current command parameters
- **THEN** the command fails with a clear error

### Requirement: Per-item Retry Limit
The system SHALL track attempts per item and cap retries using `--queue-retries` (default 3).

#### Scenario: Retry limit reached
- **WHEN** an item fails more than the allowed retries
- **THEN** the item remains failed and is not retried automatically
