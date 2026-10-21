# ADR 0111: Add bounded higher-rate HTTPS reference profiles

## Status

Accepted for the recorded local Windows and bounded Linux/Windows CI environments.

## Context

The repeated local acceptance matrix exercised trusted HTTPS through the native
engine at 2, 5, and 10 journey starts/s. It reconciled client TLS handshake
counts with server TCP and request ledgers, but the evidence did not exercise
TLS delivery under a higher scheduled rate. The profile cap is 50 requests/s;
adding exact 20 and 40 starts/s profiles stays below that configured ceiling.

## Decision

Advance the calibration preflight to schema v10 and add two trusted loopback
HTTPS GET profiles at 20 and 40 starts/s. Keep the fixed 4 KiB response,
connection reuse, five-second duration, ten workers, one-second timeout, and
50 RPS maximum. Require exact planned/admitted/started/completed counts, zero
drops, matching client trace and independent server TCP connection counts,
matching request/response body ledgers, and a bounded positive handshake count
no greater than completed requests. Connection reuse can open more than one
connection when requests overlap; the verifier must reconcile the observed
ledger rather than assume exactly one. Preserve schema-v9's original
39-profile matrix for historical artifacts.

Advance the repeated acceptance manifest to schema v6 and require the complete
41-profile matrix in every independent run. The higher-rate profiles are local
generator/transport evidence only; they do not establish timing accuracy under
OS pressure, target behavior, generator maximum throughput, or application
capacity.

## Validation

Deterministic tests cover exact 20/40 HTTPS tuples, reject unmeasured rates,
reconcile handshake/TCP/request/body ledgers, and retain schema-v9 artifact
compatibility. On 2026-10-21, repeated schema-v10 acceptance passed twice on
local Windows, bounded Linux CI, and (after rerunning one failed job) bounded
Windows CI. Each passing manifest reconciled 41 exact profiles, both controlled
negative cases and ten transport cases. The first Windows CI attempt correctly
failed closed when one 40 starts/s TLS profile dropped five of 200 scheduled
starts; the 195 delivered requests reconciled fully across client, TLS, TCP and
body ledgers. The Windows CI rerun passed both repeats. This observed sensitivity
to hosted-runner scheduling load remains a limitation; same-host repeats do not
establish cross-host or statistical independence.
