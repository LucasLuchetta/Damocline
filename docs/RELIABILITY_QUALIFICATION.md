# Reliability Qualification (RQ) milestone

**Priority:** RQ takes precedence over new features. New capability work may
merge only when it closes an RQ gap, is required by an RQ criterion, or is a
safety fix. See [ADR 0181](adr/0181-reliability-qualification-milestone.md).

**Why:** G0 is closed **within the local reference scope** ([G0 status](G0_STATUS.md)).
That shows the generator and accounting behave correctly on controlled loopback
fixtures. It does **not** show that Damocline gives correct, reproducible
capacity conclusions for representative applications on real servers and
networks. RQ closes that gap before the scope widens.

## Evidence levels for claims

Every capability claim in docs, changelog entries, reports and PRs must state
the highest level it has reached. A higher level requires all lower levels.

| Level | Meaning | Minimum evidence |
| --- | --- | --- |
| `IMPLEMENTED` | Code is merged and covered by deterministic tests within its documented scope. | Tests in `go test ./...`; documented limitations. |
| `VALIDATED_CONTROLLED` | Results agree with an independent reference (server-side ledger, known fixture or another tool) in a controlled environment, across repeated runs, including deliberate fault cases. | Recorded protocol, repeated runs, independent reference, falsification attempts that failed to break the claim. |
| `PROVEN_REPRESENTATIVE` | The `VALIDATED_CONTROLLED` result is reproduced on representative applications in at least two environments (servers, networks, load levels), with the conservative-conclusion rules holding. | The validation protocol below, completed in two environments. |

No capability is `PROVEN_REPRESENTATIVE` today. An unlabeled claim must be
read as `IMPLEMENTED` at most.

## Acceptance criteria and status

Status values: `NOT_STARTED`, `IN_PROGRESS`, `MET` (with a link to evidence).
A criterion is `MET` only when every listed check has recorded evidence.

### RQ-1 (P0) — CI stabilization

**Status:** `IN_PROGRESS` — CI exists, but some intermittent failures were
rerun without a recorded root cause (see hosted CI notes in [G0 status](G0_STATUS.md)).

- [ ] The full CI workflow runs at least 20 consecutive times on `master`
      on every hosted OS with no failure, **or** every failure has a recorded
      root cause and a merged fix.
- [ ] No test is skipped, retried or marked flaky to make CI green without a
      linked issue explaining why.
- [ ] Timing-sensitive tests state their tolerance and the reason for it.
- [ ] `go test -race -count=3 ./...` passes on Linux and Windows.

### RQ-2 (P0) — Independent reference matrix

**Status:** `IN_PROGRESS` — loopback ledgers cover many cases
([Reference validation](REFERENCE_VALIDATION.md)); the matrix is not yet
complete or independent of the code under test for every row.

- [ ] A published matrix of known scenarios whose expected request counts,
      journey counts, latency (injected delays), error classes,
      cancellations and configured limits are known in advance.
- [ ] Each row is checked against a ledger that shares no accounting code
      with the engine.
- [ ] Every row passes on repeated runs within a predeclared tolerance; any
      disagreement fails the row.
- [ ] Rows cover normal success, injected latency, HTTP errors, transport
      failures, timeouts, cancellation, worker and rate limits, and
      generator saturation.

### RQ-3 (P0) — Representative applications

**Status:** `NOT_STARTED`

- [ ] At least three representative journeys (see protocol) are tested
      across at least two servers/networks and all four load levels.
- [ ] Results are reproducible: repetition-level intervals of repeated
      campaigns of the same configuration agree, or the difference is
      explained.
- [ ] Damocline counts and latencies are cross-checked against server-side
      counters (access logs or application metrics) and against k6 under the
      **same offered load and the same success rules**.
- [ ] Damocline never reports a load level as sustainable when server-side
      evidence or the reference tool shows it was not.
- [ ] When the generator is the limit (CPU, scheduling lag, dropped starts,
      connection pool), Damocline reports a generator limit and never
      attributes it to the server. Verified with a deliberately constrained
      generator.

### RQ-4 (P0) — Authenticated journeys

**Status:** `NOT_STARTED`

- [ ] Concurrent synthetic users never share cookies, tokens or server-side
      sessions, verified by the server recording which identity served each
      request.
- [ ] Per-user state (for example a cart or draft) stays consistent within
      each user's journey and is never observed by another user.
- [ ] The observed operation distribution matches the configured weights
      within a predeclared tolerance, verified from server-side counts.

### RQ-5 (P1) — Campaign-level statistics

**Status:** `IN_PROGRESS` — repetition-level p95 verdicts exist
([ADR 0177](adr/0177-robust-p95-verdict-and-repetition-independence.md));
block-level methods and their validation are incomplete.

- [ ] Capacity verdicts use intervals computed at block or repetition level,
      not over individually autocorrelated requests.
- [ ] Each method states its assumptions and is validated on synthetic
      series with known autocorrelation and known run-to-run variance;
      nominal coverage is checked by simulation.
- [ ] Too few repetitions or blocks yield `INCONCLUSIVE`.

### RQ-6 (P1) — Conservative conclusions

**Status:** `IN_PROGRESS`

- [ ] Insufficient, degraded or missing evidence (generator health, network
      path, sample size, telemetry) can never produce an approval, a
      sustainable-capacity statement or an infrastructure recommendation.
- [ ] Each such path has a deterministic test asserting `INCONCLUSIVE` or an
      equivalent non-approval state.
- [ ] Reports separate measured facts from hypotheses about root cause.

## Validation protocol

The numbers below are a **starting proposal** chosen to make the first
qualification feasible and reviewable. They are not universal statistical
limits and must be revised when evidence (for example observed run-to-run
variance) shows they are insufficient.

1. **Journeys:** three representative journeys, for example a read-heavy
   browse journey, a mixed authenticated journey with writes, and a heavier
   search or report journey. Public artifacts use synthetic or disposable
   applications, documentation domains (`app.example.com`) or loopback.
2. **Repetitions:** five repetitions per configuration
   (journey x load level x environment).
3. **Load levels:** light, intermediate, saturated (beyond the expected knee)
   and recovery (back to light after saturation).
4. **Generator:** runs on a separate machine from the target, with CPU,
   memory, scheduling lag and dropped starts monitored and recorded.
5. **Reference checks:** server-side counters and k6 under the same offered
   load and success rules for every configuration.
6. **Deliberate faults:** injected slowness, unavailability (for example 503
   responses or refused connections) and interruption (target or network cut
   mid-run). Damocline must report each correctly and must not approve the
   affected levels.
7. **Inconclusive by default:** when the generator, the network path or the
   number of samples cannot support a conclusion, the result is
   `INCONCLUSIVE`, never a pass.
8. **Second environment:** repeat the whole protocol in a second environment
   (different server and network). Only results that hold in both may be
   called `PROVEN_REPRESENTATIVE`.

Private targets, hostnames and reports stay local and gitignored. Public
evidence records only synthetic fixtures, aggregated results and hashes.
