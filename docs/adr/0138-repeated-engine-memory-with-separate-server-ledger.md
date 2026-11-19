# ADR 0138: observe repeated engine memory with a separate server ledger

## Status

Accepted for bounded G0-07/G0-09 integration evidence.

## Context

Writer allocation tests do not measure engine workers, HTTP response buffers,
transport cleanup or metric aggregation. An in-process server would also add
its own memory to the generator high-water counter.

## Decision

Add an explicit integration test using a fresh generator child and a parent-owned
loopback HTTP server. Execute four runs in the same child, with the first run as
warm-up and three subsequent retained-heap observations. Each run configures 50
workers, 100 scheduled journey starts/s for two seconds, a 100 HTTP-starts/s
governor, a three-second request timeout and one GET step returning exactly
2 MiB. Public workload ceilings remain unchanged.

For the first 50 requests in each run, the server sends all but the final body
byte, flushes the response prefix, and waits until all 50 prefixes have been
sent. This establishes overlapping response handlers while exercising large
body reads. It does not directly measure simultaneous client buffer residency.
The server admits at most 200 requests per run and reuses one response buffer.
The parent deadline is 45 seconds; the child's engine batch has a 35-second
context. Both server waits and child execution are bounded.

Reconcile scheduled starts into admitted or busy/late drops, every admission
into a completed successful iteration, all client requests against independent
server received/completed counts, and complete response bytes on both sides.
Require at least 50 admitted requests and overlapping server handlers in every
run. Reject cancellations, HTTP failures, incomplete and oversized bodies.
Keep persisted and recomputed quality LIMITED. Drops are explicit observations,
not evidence of delivered throughput or server rejection.

After each engine call returns and its result leaves scope, collect twice to
discard sync.Pool victim caches and read runtime.MemStats. Each measured run's
retained HeapAlloc must stay within 16 MiB above the post-warm-up value. Record
all four values. The generator image's OS RSS high-water mark must be positive
and no greater than the predeclared 512 MiB fixture screen. Use the existing
Windows PeakWorkingSetSize or Linux /proc/self/status VmHWM helper and record its
method. Do not reset counters or include the parent server's process memory.

Run without race instrumentation: race runtime memory is outside this resource
screen. Unsupported operating systems explicitly skip the fixture.

## Consequences

This tests aggregate workers at the configured public worker/rate ceilings and
the accepted response-size boundary, including repeated transport and collector
teardown. It adds no product command, schema field, dependency or permission.
The child observation includes generator runtime, test harness and identity
preparation. Server process costs remain separate, though both processes still
share host CPU, memory and loopback networking.

The heap and RSS limits are regression screens for this bounded workload, not
universal memory ceilings or evidence of long-duration leak freedom. Collection
between runs changes GC conditions. No low-memory/OOM intervention, arbitrary
response distribution, filesystem failure, physical-host independence or target
capacity qualification follows from a passing result. Actual emitted logs and
their runtime/source identity are required evidence of execution.

## Reproduction

```text
go test -tags integration ./internal/engine -run '^TestNativeEngineRepeatedMemoryWithSeparateServerLedger$' -count=1 -timeout=1m -v
```
