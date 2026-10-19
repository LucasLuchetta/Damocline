# ADR 0090: Select weighted workflows at every journey start

## Status

Accepted (experimental).

## Context

Weighted profiles are intended to describe journey-start proportions. Open-arrival runs already chose a profile for each scheduled arrival, but closed-loop runs assigned each VU one profile for the entire test. A slow or longer workflow therefore generated fewer journey starts than its weight implied. The report's `assigned_vus` also presented actor allocation beside a weight that was documented as a start share.

## Decision

For new runs with explicit profiles, use the same synchronized smooth weighted round-robin selector at each closed-loop journey start and at each open-arrival admission callback. The weights describe selected starts only; they do not describe VU share, elapsed time, HTTP request share, or completed-work share. Open-arrival slots dropped before profile selection because they were late or cancelled remain unassigned. Slots passed to the admission callback retain a profile even when a worker is busy.

Advance the HTTP report schema to v26. Persist `profile_selection: per_journey_start`, per-profile selected-start counts, distinct VUs observed per profile, and the existing worker-started/completed/failed/cancelled journey ledger. Current-schema validation reconciles profile selection totals with scheduler admissions plus worker-busy drops for open arrivals, and profile starts with engine starts. Include the selection model in the workload fingerprint and redacted export. Preserve `assigned_vus` and its historical meaning when reading older reports.

Each VU retains its own cookie jar as it changes workflows. This preserves actor-local sessions but does not create separate authenticated identities per workflow. Existing scenario JSON remains valid; single-workflow scenarios are unchanged.

## Consequences

Different workflow latency or request count no longer biases the selected journey-start mix in closed-loop runs. Selection is shared across actors, so a workflow may execute on any VU and that VU's identity is reused across its workflows. Busy open-arrival drops and incomplete journeys remain separately visible. Scheduler lateness, target behavior, generator quality, and workload representativeness still limit capacity conclusions. Historical reports retain their former semantics and are not reinterpreted.
