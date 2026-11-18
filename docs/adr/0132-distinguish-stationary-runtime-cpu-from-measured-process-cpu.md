# ADR 0132: distinguish stationary runtime CPU from measured process CPU

## Status

Accepted for schema-v33 generator CPU evidence.

## Context

A bounded native-engine fixture generated four seconds of controlled CPU work
with GOMAXPROCS=1. All 20 loopback requests reconciled and OS process CPU was
about one core, while Go CPU-class counters stayed at their prior values. The
installed Go runtime's CPU statistics reader uses its stop-the-world snapshot;
reading those counters does not itself refresh them. Treating a zero counter
delta as a valid idle observation was therefore incorrect.

Requiring that optional estimate in addition to valid process CPU also made
ordinary quiet campaigns depend on whether a GC snapshot happened to advance.

## Decision

Require strictly advancing total runtime CPU counters and a nonnegative,
arithmetically possible idle delta before setting `go_cpu_valid`. Stationary or
invalid deltas leave the estimate unavailable with zero numeric fields. Do not
force garbage collection to manufacture a fresh observation. This detects the
stationary case; it does not prove complete snapshot/window alignment.

Emit run schema v33. For this version, CPU source availability requires OS
process CPU readings and the existing sample counts. An unavailable Go
CPU-class estimate remains an optional diagnostic and does not substitute for
or invalidate independently measured process CPU. Keep every existing process
CPU threshold, entitlement, sampler coverage/cost, memory, scheduler, workload,
independent-server-ledger and controlled-reference identity gate. Valid high Go
CPU estimates remain conservative pressure findings. Unavailable estimates
retaining numeric observations are invalid in v33. Earlier report versions
retain their original CPU availability requirement; no historical files are
rewritten or upgraded.

CPU diagnostics evaluate each available source independently. A missing source
can coexist with a client-only pressure signal supported by the other source;
missing telemetry cannot hide measured process pressure, and neither source
establishes target CPU or a server bottleneck. Ordinary external-run quality
remains LIMITED. Only the existing exact controlled calibration fixture can
reach positive quality with all its other evidence intact.

The integration fixture runs a child with GOMAXPROCS=1, a 20-second parent
deadline, four seconds at five scheduled journey starts/s, a 100 request-starts/s
ceiling and bounded CPU work capped at six seconds. Require verified one-core
normalization, measured mean process CPU at least 90%, all 20 independent server
requests reconciled, complete scheduler lag samples, the client-only CPU signal
and recomputed LIMITED quality. Stop and join the worker after the observation.
The process includes the small loopback server; this is local synthetic CPU
work, not external OS-load calibration or isolated production-generator cost.

## Consequences

An unresolved runtime estimate is now explicit rather than appearing as idle
CPU. Positive controlled-fixture headroom still requires actual OS CPU evidence
and all other quality gates. Process-normalization and short-interval CPU
uncertainty remain visible; ratios above 100% are not silently clamped.

Longer CPU windows, GC-snapshot freshness/alignment, host-class timing coverage,
and target-side attribution remain open. Older saved summaries lack raw counter
deltas and cannot retroactively establish source progress. The new schema has a
reader/golden fixture and does not claim universal capacity certification.
