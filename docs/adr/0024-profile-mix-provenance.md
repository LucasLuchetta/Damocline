# ADR 0024: Record declared workload-mix provenance

## Status

Accepted.

## Context

Weighted workflow profiles describe intended journey-start proportions. Without provenance, a report cannot distinguish an assumed mix from proportions a user says were derived from sanitized aggregate usage or a business estimate. Storing arbitrary source text could also expose private details.

## Decision

Scenarios accept `profile_mix_basis` with `assumption` (default), `sanitized_analytics`, or `business_estimate`. Non-assumption bases require a short safe `profile_mix_reference` identifier. Schema-v9 reports preserve both fields, and controlled comparisons require them to match.

## Consequences

These fields are self-declarations. They do not verify source data, freshness, validity horizon, or representativeness. Raw events, domains, paths, credentials, and customer identifiers are not accepted as references. Legacy reports remain readable, but a missing basis does not match a newly declared basis for controlled comparison.
