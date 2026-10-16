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

The `net` plugin SHALL apply an explicit redirect policy to every outbound request. The default policy SHALL NOT follow redirects, so a redirect chain reaches no host the script author did not name. A request that carries an explicit policy SHALL follow it. The plugin SHALL declare a minimum TLS version as written policy.

#### Scenario: A redirect is not followed by default

- **WHEN** a request receives a redirect response to a different host
- **THEN** the plugin SHALL return the redirect response itself and SHALL NOT issue the request to that host

#### Scenario: The redirect response is returned to the caller

- **WHEN** a request receives a redirect response under the default policy
- **THEN** the returned value SHALL carry the redirect response's status and headers

#### Scenario: A declared follow policy is honored

- **WHEN** a request is issued under a policy that permits redirects and the response redirects within the allowed destinations
- **THEN** the plugin SHALL follow the redirect under that policy

#### Scenario: The minimum TLS version is explicit

- **WHEN** the plugin builds its HTTP client
- **THEN** the client SHALL configure a minimum TLS version of 1.2 explicitly as written policy

### Requirement: Outbound request errors are redacted

An error raised by an outbound request SHALL NOT render the request URL's query string, fragment, or user information. The redacted error SHALL identify the request by its method and scheme and SHALL NOT disclose credentials carried in the URL.

#### Scenario: A connection error does not disclose the query string

- **WHEN** a request to a host that cannot be reached fails, and the URL carries a query string containing a secret
- **THEN** the error SHALL NOT contain the query string or the secret

#### Scenario: A redacted error still identifies the request

- **WHEN** any outbound request fails
- **THEN** the error SHALL state the request method and the URL scheme

#### Scenario: A successful request reports no redaction

- **WHEN** an outbound request succeeds
- **THEN** the plugin SHALL return the response and SHALL NOT redact the requested URL from the result
