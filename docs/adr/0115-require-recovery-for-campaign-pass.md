# ADR 0115: Require recovery evidence for a passing closed-loop campaign

## Status

Accepted

## Context

Closed-loop campaign execution allowed `--recovery 0s` when no spike was configured. The live campaign could then report `PASS`, while offline capacity audit required a recovery phase for every passing campaign and classified the same artifact as invalid evidence. This broke the basic promise that generated campaign evidence can be replayed and defended.

The command contract and report explain that recovery is a required evaluated phase for each trial. Open-arrival campaigns already reject recovery shorter than ten seconds.

## Decision

Closed-loop campaign validation requires a recovery duration greater than zero and no longer than three minutes. Validate the policy before loading the scenario or running any phase. Keep the offline audit requirement unchanged so live and replayed conclusions share one contract.

## Consequences

An invocation that explicitly sets recovery to zero now fails before target traffic or report creation. Existing defaults and documented examples already include recovery. Historical campaign files without recovery remain readable but cannot produce a passing capacity assessment.

Saved profiles that contain `recovery: "0s"` are rejected until the operator edits the duration. The tool does not silently increase the amount of traffic or wall time in an existing profile.

## Validation

Unit coverage proves the campaign runner is not called without recovery. CLI coverage proves preflight rejects the invocation before reading the scenario or creating an output file. Full Go tests, vet and build remain required for the PR.
