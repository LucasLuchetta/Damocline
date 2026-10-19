# ADR 0076: Preserve recovery evidence after a failed spike

## Status

Accepted

## Context

Campaign phase schedules include a targeted spike followed by a recovery probe. Stopping immediately when a spike records an objective breach loses the evidence needed to tell whether the service recovered after offered load returned to baseline.

## Decision

For current closed-loop and open-arrival campaign schemas, a valid observed spike failure is retained and the scheduled recovery phase runs. A passing recovery does not erase the spike breach: the trial and campaign remain FAIL, and later phases and repeats are skipped. Invalid delivery, insufficient evidence, aborts and inconclusive spike outcomes retain their existing terminal behavior. Closed-loop spikes require a recovery phase. Offline readers require recovery evidence after a failed spike and recompute both phase classifications. A controlled loopback integration fixture degrades only the spike phase in both campaign modes and verifies the baseline recovers while the overall result stays failed.

## Consequences

Campaign schema v8 and open-arrival campaign schema v4 identify the revised phase semantics. Earlier artifacts keep their historical stop behavior. The fixture proves the phase transition and measured recovery for a synthetic local handler; it does not establish a universal recovery-time objective or represent application-specific saturation behavior.
