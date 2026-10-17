# ADR 0012: Require declared workload identity for controlled comparisons

## Status

Accepted.

## Decision

Scenarios may include `workload_revision` and `environment` safe labels. The
run report preserves both. This original comparison rule is superseded in
part by ADR 0028: a controlled comparison now also requires an equal schema-v11
workload fingerprint, matching environment, duration, ramp-up, and rate cap.
The fingerprint covers declared routes and request/journey structure without
recording resolved secrets or environment-backed body contents.

Differences in environment remain useful to inspect, but the comparator marks
them `NOT_COMPARABLE` for controlled evidence and never infers cause.

## Consequences

Older reports and unlabeled runs remain readable, but their comparison is
limited. Labels document a declared experiment; they do not prove source
revision, deployment identity, database contents, clock alignment, or root
cause. Matched protocols and repeated campaigns remain necessary for a
capacity decision.
