## ADDED Requirements
### Requirement: Go CLI Version Command
The Go CLI SHALL provide a `version` subcommand that prints build metadata.

#### Scenario: Show build metadata
- **WHEN** a user runs `telegram-send-go version`
- **THEN** the output includes version, buildTime, gitCommit, and goVersion
