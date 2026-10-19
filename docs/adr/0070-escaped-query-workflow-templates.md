# ADR 0070: Escaped query workflow templates

## Status

Accepted (experimental)

## Context

The scenario engine already extracts bounded response values and substitutes them into complete path segments, headers, and environment-backed bodies. Query strings rejected templates, preventing common dependent read/update workflows from using server-issued identifiers in filters.

## Decision

Allow previously extracted variables in query parameter values. Parse query values before substitution and encode the resolved values with Go's query encoder. Keep path variables restricted to complete path segments and resolve every request relative to the configured base URL. Reject malformed templates and references not extracted by an earlier step.

## Consequences

The configured query structure remains fixed while data values may vary per journey. Query encoding may normalize parameter ordering and escaping. This does not add HTML extraction, cross-origin requests, arbitrary template evaluation, or dynamic login handling.
