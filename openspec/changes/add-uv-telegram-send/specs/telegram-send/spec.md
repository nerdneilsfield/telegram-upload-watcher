## ADDED Requirements
### Requirement: uv Environment Bootstrap
The system SHALL provide uv-based environment initialization via a repository-level pyproject.toml that declares Telegram runtime dependencies.

#### Scenario: New developer setup
- **WHEN** a developer runs `uv sync` in the repository root
- **THEN** dependencies are installed and the project is ready to run

### Requirement: Telegram Configuration Loading
The system SHALL load Telegram API configuration from an INI file with a [Telegram] api_url field and one or more [Token*] sections containing tokens.

#### Scenario: Config file is provided
- **WHEN** a user passes a config file path
- **THEN** the system loads api_url values and token entries from the INI file

### Requirement: URL and Token Pooling
The system SHALL select API URLs and bot tokens from configured pools and distribute usage across them for sending operations.

#### Scenario: Multiple URLs and tokens
- **WHEN** a send operation is performed with multiple configured URLs and tokens
- **THEN** the system chooses a URL and token from the pools and records usage

### Requirement: Send Text Message
The system SHALL send text messages to a target chat via sendMessage and support an optional topic/thread id.

#### Scenario: Send a message
- **WHEN** a user triggers a sendMessage call with chat_id and text
- **THEN** the message is delivered to the target chat (or an error is surfaced)

### Requirement: Send Media Group
The system SHALL send image files in batches using sendMediaGroup with a configurable group size.

#### Scenario: Send images from a directory or zip
- **WHEN** a user provides a directory or zip file containing images
- **THEN** the system batches images and uploads them using sendMediaGroup
