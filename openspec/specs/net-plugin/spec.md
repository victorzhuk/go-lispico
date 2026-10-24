# net-plugin Specification

## Purpose

The net-plugin capability provides HTTP client functionality for the system, registered and made ready for use when the system initializes.

## Requirements

### Requirement: net-plugin implementation
The system SHALL implement the net-plugin functionality as described in the proposal.

#### Scenario: Basic functionality works
- **WHEN** the system is initialized
- **THEN** the net-plugin SHALL be ready for use

### Requirement: Outbound requests follow a declared destination policy

The `net` plugin SHALL refuse redirect following for every outbound request, including requests with a per-request timeout.
The plugin SHALL return the redirect response without contacting its destination.
The plugin exposes no redirect-following option and uses Go's default TLS policy.

#### Scenario: A redirect is not followed by default

- **WHEN** a request receives a redirect response to a different host
- **THEN** the plugin SHALL return the redirect response itself and SHALL NOT issue the request to that host

#### Scenario: The redirect response is returned to the caller

- **WHEN** a request receives a redirect response under the default policy
- **THEN** the returned value SHALL carry the redirect response's status and headers

#### Scenario: Per-request timeout preserves redirect refusal

- **WHEN** a request supplies `:timeout` and receives a redirect response
- **THEN** the plugin SHALL return that response without following the redirect

### Requirement: Outbound request errors are redacted

Request-construction and transport errors SHALL omit URL user information, path, query, and fragment.
Malformed URLs SHALL use a redacted placeholder.
Request-construction errors SHALL wrap `net.ErrInvalidURL`.
Transport errors SHALL identify the request method and a redacted destination.

#### Scenario: A connection error does not disclose the query string

- **WHEN** a request to a host that cannot be reached fails, and the URL carries a query string containing a secret
- **THEN** the error SHALL NOT contain the query string or the secret

#### Scenario: A redacted error still identifies the request

- **WHEN** a transport failure contains a valid URL
- **THEN** the error SHALL identify the request method, scheme, and host without exposing other URL components

#### Scenario: A successful request reports no redaction

- **WHEN** an outbound request succeeds
- **THEN** the plugin SHALL return the response and SHALL NOT redact the requested URL from the result
