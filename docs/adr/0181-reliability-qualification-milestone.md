# ADR 0181: Reliability Qualification milestone and evidence levels

## Status

Accepted.

## Context

G0 closed within the local reference scope (ADR 0149). Feature work has since
grown faster than evidence that conclusions are correct on representative
applications, real networks and separate generator hosts. "Implemented and
tested" risks being read as "trustworthy for real applications".

## Decision

1. Create the Reliability Qualification (RQ) milestone
   ([RELIABILITY_QUALIFICATION.md](../RELIABILITY_QUALIFICATION.md)). It takes
   priority over new features.
2. Adopt the evidence levels `IMPLEMENTED`, `VALIDATED_CONTROLLED` and
   `PROVEN_REPRESENTATIVE`. Every important PR states which level its
   capability reached (pull request template).
3. Adopt this methodology for every measurement capability:
   measurement hypothesis -> independent reference -> repeated testing ->
   falsification attempt -> acceptance -> widen scope.
   A capability widens its scope only after a falsification attempt in the
   current scope failed to break it.

## Consequences

- Unrelated features wait until the RQ P0 criteria are met.
- README and roadmap claims must not exceed the stated evidence level.
- The protocol numbers (3 journeys, 5 repetitions, 4 load levels, 2
  environments) are a starting proposal and may be revised with evidence.
- This ADR changes process and documentation only; runtime behavior is
  unchanged.
