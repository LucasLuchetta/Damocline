# ADR 0091: Isolate cookie contexts by workflow and actor

## Status

Accepted (experimental).

## Context

Schema-v26 selects weighted workflows for every journey start. The default VU-local cookie jar follows an actor as it changes workflows. This is useful when workflows share a login identity, but it can misrepresent a workload where each workflow uses a different application identity. Assigning one workflow permanently to each actor would distort journey-start weights and would fail when the number of actors is smaller than the number of workflows.

## Decision

Keep the scenario-level `per_vu_cookie_env_prefix` behavior for backward compatibility. Add the same optional field to each weighted profile. When any profile declares it, construct an isolated HTTP client cookie jar for every actor/workflow pair; preload that jar from the profile prefix's numbered environment variables. A profile without a prefix in this mode receives its own initially empty jar. Resolve enough distinct actor cookies for the maximum level used by bounded exploration and for any campaign spike. Reject combination with the scenario-level prefix, literal `Cookie` headers, duplicate prefixes, missing or duplicate cookie values. Cookie contents and environment-variable names are never persisted.

Advance the HTTP report schema to v27 and add an authentication-context enum per profile: `per_actor_workflow_cookie` or `isolated_workflow_cookie_jar`. Add only the isolation mode to the workload fingerprint; resolved credentials and their environment names are excluded. Redacted exports preserve the same fixed enum. Retain the historical actor-wide jar when no profile prefix is configured.

Remove the old closed-loop requirement that VU count be at least the number of profiles. Profiles are selected per journey start; insufficient observations are handled by existing sample adequacy and quality evidence rather than assigning an actor permanently to every profile.

## Consequences

The engine can model workflows with distinct supplied cookie identities in both closed-loop and open-arrival modes while preserving the configured journey-start mix. Capacity exploration and campaign spike phases retain the same per-workflow isolation while increasing the actor count. The cookie jar remains scoped to one worker actor and one workflow; cookies do not cross between workflows. This does not record browser login, refresh credentials, validate identity permissions, or isolate application-side data. Operators remain responsible for authorized, valid, non-production identities and for secret storage.
