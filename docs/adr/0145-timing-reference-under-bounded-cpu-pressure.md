# ADR 0145: timing reference under bounded in-process CPU pressure

G0-03, G0-04, G0-06 and G0-12 lacked any fixture that compares
engine-reported timing with a separate reference while the generator host
is under controlled pressure.

`internal/engine/timing_reference_pressure_test.go` runs the native engine
in open-arrival mode (10 arrivals/s for 3 s, 30 planned arrivals) against a
loopback `httptest` server that injects a known 20 ms delay before flushing
headers. The server records, with its own monotonic timestamps, each
request's arrival offset and its hold time. A baseline runs without pressure;
the pressure case starts one busy goroutine per `GOMAXPROCS` with a hard 6 s
deadline that stops independently of the run.

Hard invariants, in both cases:

- `scheduled == admitted + dropped`, `started == admitted`, one scheduler lag
  sample per planned arrival, engine request count equals the independent
  server count, and exactly 30 arrivals are planned (no coordinated omission).
- Client first-byte phase count equals the server hold count, and the mean
  client first-byte time is not shorter than the mean server hold by more
  than 2 ms of clock slack.
- Signals stay client or client-transport attributed.

Fixture tolerances: no dropped arrivals, mean first-byte excess over server
hold <= 100 ms, server-observed lateness relative to the first arrival <=
engine maximum dispatch lag + 100 ms, scheduler lag p95 <= 50 ms and max
< 100 ms (the existing reference-soak screen). When any tolerance is
exceeded, the run must not be `VALID_WITHIN_ENVELOPE` and must carry a
`CLIENT_SCHEDULER_LATE`, `GENERATOR_CPU_PRESSURE` or
`CLIENT_CONNECTION_ACQUISITION_WAIT` signal; dropped arrivals must also be
reported as undelivered demand.

Limitations: the reference shares the process and monotonic clock with the
engine, so it is separate in measurement point, not in clock or host. The
pressure is in-process Go CPU work only; it does not model OS scheduler
contention from other processes, memory, I/O, timer coarsening or clock
error. Local runs showed that under pressure server-observed lateness can
exceed engine dispatch lag by more than 100 ms, so dispatch lag does not
bound post-dispatch delay; the fixture classifies such runs as degraded
rather than claiming accuracy. Tolerances are CI screening limits, not
validated accuracy for any hardware class.
