# ADR 0012: Require declared workload identity for controlled comparisons

## Status

Accepted.

## Decision

Scenarios may include `workload_revision` and `environment` safe labels. The
run report preserves both. The offline report comparator calls a comparison
controlled only when the workload revision is present and identical and the
target, concurrency, model, arrival rate, authentication mode, think time,
request timeout, SLOs, sample threshold, observation window, routes, and
profiles match.

The environment labels can differ because comparing configurations is useful,
but the result is explicitly non-causal.

## Consequences

Older reports and unlabeled runs remain readable, but their comparison is
limited. Labels document a declared experiment; they do not prove source
revision, deployment identity, database contents, clock alignment, or root
cause. Matched protocols and repeated campaigns remain necessary for a
capacity decision.
