# ADR 0078: Require an output artifact for campaign runs

## Status

Accepted.

## Context

Campaigns execute multiple bounded phases and repeats, but the CLI allowed normal non-dry campaigns to finish without saving their phase reports. In that case, an observed failure, delivery problem, or abort could only be inspected in terminal output and could not be reproducibly reassessed with the offline campaign tools.

## Decision

Require `--out` for every non-dry closed-loop and open-arrival campaign. Keep dry-run previews exempt because they send no traffic and produce no measured phase evidence. The existing atomic report writer stores the complete result before the CLI returns the campaign's non-PASS exit status.

## Consequences

Every controlled campaign invocation names a destination artifact before traffic starts, preserving all phases and trial outcomes returned by the campaign runner. A forced process termination may still occur before the runner returns and therefore before a final JSON artifact can be written; incremental phase journaling remains out of scope. Destination write errors are reported after the campaign and cannot establish that an artifact was retained.
