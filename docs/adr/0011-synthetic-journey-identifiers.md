# ADR 0011: Generate private synthetic identifiers per journey

## Status

Accepted.

## Decision

An authorized scenario may name an environment variable through
`synthetic_data_prefix_env`. The value must be a short safe prefix. When it is
configured, `{{synthetic_id}}` becomes available to steps in the journey. The
engine combines the prefix, run start, worker identity, and an atomic sequence
to produce a new identifier for each journey.

Identifiers stay only in the request construction variables. They are never
added to step paths in reports, logs, scenario history, or diagnostics.

## Consequences

Write workflows can target an application-specific synthetic namespace and
can include their own explicit DELETE cleanup step. Cancellation or cleanup
failure can still leave data behind, so this does not implement a universal
data lifecycle manager or authorize deletion outside the configured workflow.

The engine continues through the configured sequence after a completed request
or business assertion fails, allowing a final cleanup step to run. Cancellation,
process termination, missing dynamic values, or a failed cleanup request can
still leave data behind. The cleanup step's outcome is included in the ordinary
per-step evidence.
