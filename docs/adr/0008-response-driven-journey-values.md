# ADR 0008: Response-derived values remain local to one journey

## Status

Accepted.

## Decision

Steps may capture a bounded response header or scalar JSON Pointer value. Later steps in the same journey may substitute it into a complete path segment, a header, or `body_env` content. The scenario validator requires each variable to be declared before use and rejects query interpolation.

Values exist only in the current journey's memory. They are not added to reports, scenario history, diagnostics, or logs. Missing extraction fails the request visibly instead of using a stale value.

## Consequences

This supports common ID, pagination-token, and CSRF-like dependencies without turning scenarios into a general scripting language. It does not record login flows, persist state across journeys, or manage synthetic test-data setup and cleanup.
