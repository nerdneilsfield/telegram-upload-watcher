## ADDED Requirements
### Requirement: Proxy List Input
The system SHALL accept a `--proxy-list` file for CLI commands and load one proxy per line.

#### Scenario: Parse proxy list
- **WHEN** the user supplies `--proxy-list /path/to/proxies.txt`
- **THEN** each non-empty line is parsed into a proxy entry

### Requirement: Proxy URL Formats
The system SHALL support HTTP, SOCKS5, and SOCKS5H proxies with optional credentials, including standard and custom formats.

#### Scenario: Parse proxy credentials
- **WHEN** a line contains `http://user:pass@host:port`, `socks5://user:pass@host:port`, or `socks5h://user:pass@host:port`
- **THEN** the proxy uses the provided username and password

#### Scenario: Parse custom credentials
- **WHEN** a line contains `user@pass:host:port` (optionally prefixed with `http://`, `socks5://`, or `socks5h://`)
- **THEN** the proxy uses `user` and `pass` as credentials

#### Scenario: Distinguish socks5 and socks5h
- **WHEN** a line uses the `socks5h://` scheme
- **THEN** the proxy entry is marked for remote DNS resolution

### Requirement: Proxy Pool Initialization
The system SHALL validate proxies via `getMe` and only add successful proxies to the active pool.

#### Scenario: Activate healthy proxies
- **WHEN** proxy validation succeeds for a proxy
- **THEN** the proxy is added to the active pool

#### Scenario: Reject all proxies
- **WHEN** no proxies pass validation
- **THEN** the command exits with a clear error

### Requirement: Proxy-based Request Routing
When a proxy pool is active, the system SHALL send Telegram API requests using a proxy-selected HTTP client.

#### Scenario: Use active proxies
- **WHEN** a Telegram API request is made with an active pool
- **THEN** the request uses one of the active proxies

### Requirement: Failure Tracking and Removal
The system SHALL track consecutive proxy failures and temporarily remove proxies that exceed the failure threshold.

#### Scenario: Remove failing proxy
- **WHEN** a proxy exceeds the configured failure threshold
- **THEN** the proxy is marked inactive and removed from the active pool

### Requirement: Proxy Recovery
The system SHALL periodically re-check inactive proxies and restore them when validation succeeds.

#### Scenario: Restore proxy
- **WHEN** an inactive proxy passes the periodic validation check
- **THEN** it is returned to the active pool

### Requirement: Fallback Without Proxy List
When `--proxy-list` is not provided, the system SHALL use existing environment proxy settings or direct connections.

#### Scenario: Use system proxy
- **WHEN** `--proxy-list` is absent and environment proxy variables are set
- **THEN** requests use the system proxy configuration

#### Scenario: Direct connection
- **WHEN** `--proxy-list` is absent and no environment proxy variables are set
- **THEN** requests connect directly
