# ADR 0137: require recorded campaign objective fields

## Status

Accepted.

## Context

Closed-loop campaign v13 and open-arrival campaign v8 introduced predeclared
objectives (ADR 0108). Their always-serialized fields include an error budget
and maximum dropped arrivals for which zero is valid. Removing either field
can still produce a semantically valid decoded contract, but that zero was
not actually recorded. Root-object presence alone cannot prove the criteria.

## Decision

Share one raw objective-field presence validator between both campaign readers.
Require every persisted contract field to be present and non-null before typed
assessment, using the original introduction schemas: closed-loop v13+ and
open-arrival v8+. Keep ObjectiveContract.Validate, phase matching and policy
replay responsible for semantic checks. Explicit zeros remain valid presence;
older campaign versions do not acquire an objective requirement.

Readers continue to own bounded bytes and duplicate-key rejection. Their
existing shared validation boundaries cover audit, planning, collections,
dashboard, profile verification and descriptive export as applicable to each
campaign family. Missing criteria fail before assessment/report outputs are
written. Producers, schema numbers, traffic limits and original source files
remain unchanged.

## Validation and limitations

A mutation matrix removes/nulls every serialized objective field in both
families and checks introduction/current schemas, explicit zero controls and
older schemas without a contract. A regression demonstrates that ordinary typed
validation alone accepts the omitted zero-valued criteria. An offline CLI test
checks that incomplete criteria produce neither assessment JSON nor Markdown.
Compile overlays restoring the old reader bindings must reproduce acceptance.

This proves recorded-field presence, not when criteria were chosen, who authored
them, producer authenticity, workload representativeness or application capacity.
Other G0 artifact contracts and measurement evidence remain open.
