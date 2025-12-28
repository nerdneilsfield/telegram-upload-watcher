## ADDED Requirements
### Requirement: Send File/Video/Audio Commands
The system SHALL provide `send-file`, `send-video`, and `send-audio` commands that send a single item per API call using the Telegram `sendDocument`, `sendVideo`, and `sendAudio` methods.

#### Scenario: Send a single file
- **WHEN** a user runs `send-file` with a file path
- **THEN** the system sends the file via `sendDocument` and reports success/failure

#### Scenario: Send a video file
- **WHEN** a user runs `send-video` with a video path
- **THEN** the system sends the video via `sendVideo` and reports success/failure

#### Scenario: Send an audio file
- **WHEN** a user runs `send-audio` with an audio path
- **THEN** the system sends the audio via `sendAudio` and reports success/failure

### Requirement: Folder and Zip Traversal
The system SHALL allow the new commands to send files from directories or zip files using include/exclude patterns and zip passwords.

#### Scenario: Send files from a directory
- **WHEN** a user provides a directory with include/exclude rules
- **THEN** matching files are sent one-by-one using the selected send type

#### Scenario: Send files from a zip
- **WHEN** a user provides a zip file and optional passwords
- **THEN** matching entries are sent one-by-one using the selected send type, and password failures skip the zip

### Requirement: Watch Mode Type Selection
The system SHALL support `--with-image`, `--with-video`, `--with-audio`, and `--all` flags (combinable) for watch mode so queued items are dispatched using `sendDocument`, `sendVideo`, or `sendAudio` as configured, while images continue to use the existing media-group pipeline.

#### Scenario: Watch directory for videos
- **WHEN** watch mode is configured with `--with-video`
- **THEN** newly detected video files are enqueued and sent via `sendVideo`

#### Scenario: Watch directory for images and videos
- **WHEN** watch mode is configured with `--with-image` and `--with-video`
- **THEN** newly detected image and video files are enqueued and sent with their respective methods

#### Scenario: Watch directory for all file types
- **WHEN** watch mode is configured with `--all`
- **THEN** all matching files are enqueued; images are sent in media groups and other files use `sendDocument` or their media-specific methods
