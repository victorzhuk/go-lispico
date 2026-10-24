# llm-plugin Specification

## Purpose

The llm-plugin capability provides LLM API binding functionality for the system, registered and made ready for use when the system initializes.

## Requirements

### Requirement: llm-plugin implementation
The system SHALL implement the llm-plugin functionality as described in the proposal.

#### Scenario: Basic functionality works
- **WHEN** the system is initialized
- **THEN** the llm-plugin SHALL be ready for use

### Requirement: Provider responses are bounded and errors omit bodies

The built-in HTTP client SHALL cap completion and embedding response bodies at 8 MiB.
Streaming SHALL cap total scanned response bytes at 8 MiB, including framing bytes.
Provider HTTP error messages SHALL omit response bodies.

#### Scenario: Oversized completion or embedding response fails

- **WHEN** a completion or embedding response body exceeds 8 MiB
- **THEN** the client SHALL return an error instead of decoding the oversized body

#### Scenario: Oversized stream terminates

- **WHEN** scanned streaming response bytes exceed 8 MiB
- **THEN** the client SHALL send a terminal error chunk
- **AND** if cancellation interrupts delivery, the chunk SHALL contain the context error instead
- **AND** the client SHALL close the response body and channel

#### Scenario: Provider error body stays private

- **WHEN** the provider returns a non-success HTTP status
- **THEN** the error SHALL identify the status without including the response body

### Requirement: Streaming delivery respects cancellation

Streaming channel sends SHALL observe caller cancellation.
Callers that stop consuming SHALL cancel the context to release blocked delivery.
Cancellation SHALL terminate the stream with an error, not successful channel exhaustion.
The client SHALL queue a terminal context-error chunk without waiting for the consumer.
The client MAY discard unread canceled-stream data to make room.
Public `llm/stream` SHALL return the context error.

#### Scenario: Canceled consumer releases delivery

- **WHEN** the channel cannot accept another chunk and the caller cancels its context
- **THEN** the client SHALL queue a chunk with `Done: true` and the context error without blocking
- **AND** the client MAY discard unread canceled-stream data if the buffer is full
- **AND** the client SHALL close the response body and channel
- **AND** public `llm/stream` SHALL return the context error instead of success

### Requirement: Streaming requires explicit completion

The built-in HTTP client SHALL treat `[DONE]` or a nonempty `finish_reason` as successful completion.
It SHALL emit exactly one terminal Done chunk for successful completion.
Clean upstream EOF without either completion signal SHALL produce a terminal error chunk.

#### Scenario: Clean EOF before completion fails

- **WHEN** upstream closes cleanly before `[DONE]` or a nonempty `finish_reason`
- **THEN** the client SHALL emit a chunk with `Done: true` and error `stream ended without completion`
- **AND** public `llm/stream` SHALL return that error instead of success

#### Scenario: Finish reason followed by DONE completes once

- **WHEN** upstream sends a nonempty `finish_reason` followed by `[DONE]`
- **THEN** the client SHALL emit exactly one terminal chunk with `Done: true` and no error
- **AND** the terminal chunk SHALL preserve any content accompanying `finish_reason`
