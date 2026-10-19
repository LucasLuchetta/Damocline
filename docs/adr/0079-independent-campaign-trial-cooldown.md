# ADR 0079: Persist independent campaign trial cooldowns and IDs

## Status

Accepted.

## Context

Repeated campaign phases previously ran contiguously and had no stable identifier to group the phases belonging to each trial. This made recovery time between observations implicit and weakened offline integrity checks. Campaigns also cannot assume that application-specific data reset or authenticated session rotation is available generically.

## Decision

Add an optional operator-selected cooldown from zero through five minutes between passing trials in closed-loop and open-arrival campaigns. Include all configured pauses in planned and validated wall-time budgets. Assign each trial a cryptographically random 128-bit ID and persist it in the campaign artifact. Offline assessors validate the schema, cooldown range, and ID format/uniqueness. Cancellation during cooldown is retained as an aborted next trial.

## Consequences

Reports identify trial boundaries and state the requested recovery interval, with the schedule reproducible offline. IDs only group phases within a single artifact; they do not verify host, environment, data-reset, or authenticated-session independence. Application-specific resets and identity rotation remain future work. The campaign schema versions advance, while readers retain historical behavior for older artifacts.
