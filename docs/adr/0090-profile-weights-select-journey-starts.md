# ADR 0090: Select weighted workflows at every journey start

## Status

Accepted (experimental).

## Context

Weighted profiles are intended to describe journey-start proportions. Open-arrival runs already chose a profile for each scheduled arrival, but closed-loop runs assigned each VU one profile for the entire test. A slow or longer workflow therefore generated fewer journey starts than its weight implied. The report's `assigned_vus` also presented actor allocation beside a weight that was documented as a start share.

## Decision

For new runs with explicit profiles, use the same synchronized smooth weighted round-robin selector at each closed-loop journey start and at each open-arrival admission callback. The weights describe selected starts only; they do not describe VU share, elapsed time, HTTP request share, or completed-work share. Open-arrival slots dropped before profile selection because they were late or cancelled remain unassigned. Slots passed to the admission callback retain a profile even when a worker is busy.

Advance the HTTP report schema to v26. Persist `profile_selection: per_journey_start`, per-profile selected-start counts, distinct VUs observed per profile, and the existing worker-started/completed/failed/cancelled journey ledger. Current-schema validation reconciles profile selection totals with scheduler admissions plus worker-busy drops for open arrivals, and profile starts with engine starts. Include the selection model in the workload fingerprint and redacted export. Preserve `assigned_vus` and its historical meaning when reading older reports.

By default each VU retains its own cookie jar as it changes workflows. Scenarios may opt into separate per-workflow actor sessions with `profile_cookie_env_prefixes`, mapping every workflow name to a distinct environment-variable prefix. The engine then keeps one jar for each workflow/actor pair in both workload modes. Prefixes must resolve a complete set of distinct cookie headers for all configured actors; normalized cookie sets must also be unique across workflows for the same actor. Workflow-scoped cookies cannot be combined with scenario-wide `per_vu_cookie_env_prefix` or literal Cookie headers. Reports store only the names of scoped workflows, and workload fingerprints include those scope names but never cookie values or environment-variable names.

Advance the HTTP report schema to v27. Validate that reported session scope names refer to configured workflows, contain no duplicates, and are absent from older schema versions. Existing scenario JSON remains valid; unscoped sessions retain their previous behavior.

## Consequences

Different workflow latency or request count no longer biases the selected journey-start mix in closed-loop runs. Selection is shared across actors; per-workflow session scopes keep their cookie state separate, while the default continues to reuse each actor's session across workflows. Cookie separation does not authenticate distinct accounts or establish permission equivalence. Busy open-arrival drops and incomplete journeys remain separately visible. Scheduler lateness, target behavior, generator quality, and workload representativeness still limit capacity conclusions. Historical reports retain their former semantics and are not reinterpreted.
