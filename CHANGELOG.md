# Changelog

This project follows a human-maintained changelog. No release exists yet.

## [Unreleased]

### Added
- Add a concurrent loopback fixture that cross-checks each actor's cookie against the response-derived ID used by that actor's next request.
- Mark every non-GET/HEAD route as `MUTATING` in CLI previews, prove synthetic IDs are unique per journey, and test that configured cleanup still runs after a failed business postcondition.
- Extend loopback coverage to prove per-journey synthetic write IDs are unique and explicit cleanup steps run after business postcondition failures; document when cancellation or cleanup failure can leave synthetic data behind.
- Support bounded JSON Pointer scalar postconditions on each workflow step, classify false HTTP-200 business outcomes as failures, include assertions in workload fingerprints, and keep assertion values out of reports.
- Allow same-journey extracted values in query parameter values with query-component escaping, malformed-template rejection, and loopback coverage proving special characters cannot add parameters or change the request path.
- Preserve probe-level reasons in exploration schema v2 and CLI output; current-schema accounting failures, invalid quality, dropped arrivals and cancelled HTTP/business journeys stop a VU level as INCONCLUSIVE instead of counting it as application PASS/FAIL. Share conservative generator evidence checks between exploration and exact business-goal assessment so generator-bound failures cannot form an application-capacity bracket.
- Add optional final campaign soak (`--soak`, 10s–3m) after all independent trials; include its drain allowance in the 15-minute wall-time cap, persist schema-v4 stage evidence, validate it offline, and screen full journey/window SLOs and coarse drift. This remains bounded local campaign evidence, not a capacity certification.
- Classify current-schema campaign PASS/FAIL phases as invalid delivery when generator health, sustained CPU headroom, or configured client-rate headroom is inadequate; use mean process CPU so an isolated diagnostic peak does not reject a phase.
- Persist bounded configurable campaign drift-screen policies in campaign schema v5, reject policies that cannot be supported by the declared observation windows, and recompute the exact policy during offline audit and cross-level comparability checks.
- Plot aggregate request p95/p99 across saved observation windows in the loopback-only report dashboard, and show request/journey outcome counts per window plus clearly labeled run-wide dropped-start and generator summaries.
- Label request p95 separately from completed-journey p95 in run output, show when there are no completed journeys, and state that HTTP-only measurements do not include browser rendering or full page load.
- Aggregate two to twenty offline campaign assessments across tested workload levels only when their declared fingerprints and controls match; retain individual evidence, detect non-monotonic pass/fail observations, explain per-workflow SLO breaches by phase and measured values, and evaluate demand only at its exact tested level.
- Keep measurement-envelope quality, workload-representativeness declarations, and decision confidence as independent fields in offline campaign assessment artifacts; prevent favorable measurement evidence from upgrading unverified workload or decision dimensions.
- Report successful configured journey throughput separately from HTTP requests per second, preserve completed failures and cancelled journeys as distinct counts, and summarize successful journeys-per-second spread across campaign trials in campaign schema v3.
- Combine offline campaign evidence audit and optional business-demand assessment in a traceable JSON artifact with campaign and phase digests, workload fingerprints, observed duration and levels, visible limiting workflows, and explicit confidence limitations.
- Advance HTTP run reports to schema v22 with completed-journey metrics in every observation window; evaluate journey SLOs in engine outcomes and campaign audits, reconcile journey ledgers in offline demand assessments, and include full-journey sample adequacy/drift in campaign screening.
- Record schema-v2 campaign stage classifications and an observation-window drift screen; reserve bounded phase drains in the default wall-time cap and validate the exact saved phase schedule offline.
- Add seeded discrete between-step timing with bounded repeated samples, profile overrides, report provenance and reproducible workload fingerprints.
- Add bounded seeded between-step timing ranges for scenarios and weighted profiles, record policy and seed in schema-v21 reports and workload fingerprints, and keep fixed-pause scenarios compatible.
- Audit merged measurement/workload/campaign capabilities against the remaining acceptance gates and group the next work into integrated delivery blocks.
- Reconcile the G0 summary with the recorded schema-v7 Docker Desktop/WSL2 acceptance repeats and merged PRs through #88, retaining the bare-metal and cross-host limitations.
- Reconcile native-engine response-body accounting at the exact configured read limit and one byte above it with independent loopback server counters.
- Report DNS-resolution and connection-establishment failures as versioned transport subcategories; preserve unknown semantics for reports older than schema v20.
- Run Linux CI calibration inside a bounded container so cgroup CPU capacity is verifiable; use deterministic CPU-capacity fixtures in positive unit tests.
- Validate hierarchical quota handling with two bounded Docker Desktop/WSL2 acceptance repeats: all 13 gates per run passed, including 980/980 independent loopback requests and 10/10 transport cases. Evidence remains local and gitignored.
- Walk Linux cgroup v1/v2 CPU quota ancestors and use the tightest finite quota, bounded by logical CPUs and GOMAXPROCS; persist logical CPU count in report schema v19 and fail closed on incomplete ancestor evidence.
- Record effective generator CPU entitlement separately from logical CPUs and GOMAXPROCS; normalize Linux process/runtime CPU observations against cgroup v1/v2 quotas when verifiable, and fail closed on unknown capacity for positive quality.
- Advance report schema to v18, calibration preflight to schema v7, and repeated acceptance manifest to v3 with effective CPU capacity/source evidence and exact runtime-equivalence checks.
- Add Linux Docker Desktop/WSL2 runtime evidence: two bounded loopback acceptance repeats passed with a 4-core cgroup quota, 12 logical CPUs, and GOMAXPROCS=12; this is not bare-metal or cross-host validation.
- Advance preflight calibration to schema v6 with independent per-profile counts of distinct accepted TCP connections; require one connection for the serialized reusable fixture and one per request for `new_per_request`, while preserving schema-v1 through v5 readers.
- Advance the repeated acceptance manifest to schema v2 and require every exact request-shape, connection-policy, and arrival-rate profile to reconcile independent TCP connection counts alongside requests and bytes.
- Expand the calibration reference matrix to schema v5 with exact five-shape coverage for both reusable and new-per-request HTTP connections at 2, 5, and 10 arrivals/s; preserve archived v3 and v4 validation and require exact control-tuple matching in offline coverage.
- Reconcile aggregate calibrator server counts from each profile's recorded duration so mixed five-second and two-second workload windows cannot be miscounted.
- Fix the first schema-v5 runtime discrepancy: the aggregate ledger had treated all 30 profiles as five seconds, overstating expected server requests by 255.
- Extend offline request-mix recognition to the calibrated single synthetic POST shape; continue matching its body only by local byte length and never persist its contents.
- Add a bounded end-to-end G0 acceptance command that repeats calibrations in fresh processes, independently verifies exact profile/rate and soak ledgers, checks overload and HTTP-failure controls, runs the local transport matrix, and preserves per-attempt reports and logs on failures.
- Record descriptive p95 spread across acceptance repeats without presenting it as statistical confidence; require each persisted controlled-soak quality result to recompute from its stored report before the lab can pass.
- Expand calibration schema v4 into 15 native-engine request-shape/rate pairs (five exact shapes at 2, 5, and 10 arrivals/s), reconciled per run with independent loopback server counters; preserve schema-v3 artifacts and their narrower coverage.
- Require loopback calibration stages to contain a recognized `VALID_WITHIN_ENVELOPE` or `LIMITED` state; missing, unknown, or `INVALID` measurement quality fails calibration.
- Limit recomputed measurement quality when generator-health snapshot collection occupies at least 5% of the observation window; document this as a wall-time guardrail rather than CPU-overhead calibration.
- Add native-engine loopback tests for resolver-injected DNS NXDOMAIN, connection refusal, and TCP reset; keep DNS queries and accepted TCP sockets distinct from HTTP request receipt.
- Add a schema-v3 workload-shape calibration matrix executed through the native HTTP engine for exact 0/1/4/64 KiB GET response profiles and a synthetic 4 KiB POST body; reconcile arrivals and client body observations with independent child-fixture request and byte counters.
- Preserve sub-microsecond `time.Duration` observations when converting HTTP trace totals to fractional milliseconds, avoiding zero-valued phase sums from integer microsecond truncation.
- Require every healthy generator-calibration stage to reconcile complete response counts and exact 4 KiB min/max/total bytes in both aggregate and per-step engine reports before the fixture is accepted.
- Require the controlled 10-arrival/s soak to reconcile every scheduler lag sample, use the engine's canonical dispatch-lag method identifier, and stay within fixture-specific p95 (<=50 ms) and maximum (<100 ms) screening limits before calibration or report recomputation can accept `VALID_WITHIN_ENVELOPE`.
- Emit schema-v17 scheduler-admission counts and reject current reports where planned arrivals, admissions, worker-started iterations and drop categories do not reconcile.
- Emit schema-v16 arrival accounting with externally cancelled planned starts separated from scheduler-late and worker-busy drops; preserve missing-versus-zero semantics for historical reports.
- Reconcile current-schema started iterations with completed journeys and explicitly cancelled journeys during report-quality recomputation; reject corrupted cross-ledger totals as `INVALID` while retaining incomplete but fully accounted runs as `LIMITED`.
- Exercise a successful trusted local HTTPS request through the native engine and reconcile received and completed requests against an independent server ledger; this validates fixture behavior, not a TLS performance envelope.
- Add schema-v15 controlled-reference evidence for the calibrator-owned soak, recompute quality from the exact fixture and independent child-server counters, and require a passing calibration command to produce `VALID_WITHIN_ENVELOPE` for that controlled run.
- Identify calibration runners by exact executable SHA-256 when Go build metadata does not include a source revision.
- Add a schema-v15 report golden and negative quality tests for unattached or altered controlled evidence.

- Add bounded `connection_policy` choices (`reuse` and `new_per_request`), apply the choice to the HTTP transport, include non-default policy in workload fingerprints and schema-v14 reports, and keep the uncalibrated new-connection policy outside preflight coverage.
- Exercise an explicit zero-byte response through the native engine reference matrix and reconcile its body-byte ledger with the controlled fixture for every payload-size case.
- Recompute the calibration workload fingerprint from report-persisted step metadata for the narrow single `GET /work` reference subset, reject altered saved step fields, and keep omitted workload details unverified.
- Add JSON golden fixtures and round-trip tests for every supported HTTP report schema v1-v13, document versioned field semantics, and verify that missing legacy body observations remain unknown rather than becoming measured zero-byte bodies.
- Centralize the current run-report schema at v13, let the insights reader preserve v13 window ledgers, and continue rejecting unknown future report schemas.
- Upgrade HTTP run reports to schema v13 with per-step request and explicit profile-journey metrics in every configured observation window; fail on per-step window SLO breaches, require this evidence for positive offline demand assessments, and keep empty or undersampled windows inconclusive.
- Limit the final observation window to the configured horizon and evaluate each explicit workflow profile's completed journeys independently so a slow minority workflow cannot pass under a healthy request aggregate.
- Version calibration records with an opaque workload-shape fingerprint for healthy reference stages; compare exact declared journey shape offline and during run-quality recomputation, while keeping legacy records readable and fingerprint matches non-certifying.
- Attach a recomputed per-step observed request-latency guardrail dimension when calibration is present; require 100 successful observations per step, screen p50/p95 against the local fixture's coarse 15–150 ms bounds, and keep percentile dependence and window representativeness explicitly unverified.
- Compare complete per-step response-body sizes with the validated fixed 4 KiB preflight response and report `MATCHED`, `OUTSIDE_REFERENCE`, or `UNVERIFIED`; recompute the dimension from run fields and keep overall quality LIMITED while response timing and endpoint identity are unknown.
- Upgrade HTTP run reports to schema v12 with per-step and per-window complete, interrupted, and oversized response-body read counts; record min/max/total bytes only for complete bounded bodies, reconcile these observations with HTTP response ledgers, and never store body content.
- Share bounded calibration-record and scheduler-lag validation with the HTTP run command; `run --calibration` attaches sanitized preflight provenance and declared-dimension findings while keeping fixed-fixture response shape and endpoint identity explicitly inconclusive.
- Mark window-mean process CPU at or above 90% of recorded GOMAXPROCS capacity as LIMITED sustained generator headroom; retain interval peaks as diagnostics and use the window mean for loopback CPU acceptance.
- Report exact expected and observed dimensions for failed loopback calibration gates, including per-stage delivery, outcome, latency, generator health, lag, and independent server ledger; reject non-finite fixture latency.
- Upgrade HTTP run reports to schema v11 with a workload-definition fingerprint; label legacy, mismatched workload, or mismatched generator-preflight comparisons `NOT_COMPARABLE`, and report application revision as the observed treatment.
- Recompute measurement quality for every required phase during offline demand planning and reject persisted `VALID_WITHIN_ENVELOPE` claims that disagree with the underlying run fields.
- Reconcile six in-flight requests cancelled by an external abort during the graceful-drain interval with the independent loopback server ledger.
- Compare six requests that finish after the schedule cutoff against the independent loopback ledger, extending G0-15 evidence to graceful drain.
- Reconcile an interrupted native-engine request with the independent loopback server ledger, including received/completed/cancelled/rejected conservation.
- Upgrade HTTP run reports to schema v10 with generator sampler interval, maximum sample gap, and total/maximum sample-collection wall time; flag inconsistent sample data as INVALID and long gaps as LIMITED.
- Enforce the scenario JSON byte limit exactly at 1 MiB and reject report artifacts above 16 MiB before creating or replacing an output file, with deterministic boundary tests.
- Calibrate scheduler-lag reporting with nine bounded observations across three injected scheduler pauses, record the timing method and 50 ms fixture tolerance, and reject missing or altered lag evidence in offline preflight reads.
- Record weighted-profile mix provenance as an explicit assumption, sanitized-analytics label, or business-estimate label in schema-v9 run reports; safe references are identifiers only and do not validate representativeness.
- Upgrade HTTP run reports to schema v8 with exact status-code/family counters and structured assertion, timeout, TLS/transport, redirect, 429-response and oversized-body counts; reconcile aggregate, step and window evidence.
- Report observed between-step think-time totals separately from per-step request and completed-journey latency, including interrupted sleep intervals; retain cancelled partial journeys outside completed journey percentiles.
- Record per-step HTTP client timing totals and observation counts for rate-token wait, DNS, TCP connect, TLS handshake, first response byte, bounded body read, and full request completion/error; document that these are client observations, not server processing time.
- Upgrade HTTP run reports to schema v7 for phase timing. Quality checks reconcile new phase counts for v7 while treating those fields as unknown in earlier reports.
- Require adequate conditional P95 rank intervals, binomial error bounds, complete observation-window coverage, and variability across all independent trials before offline demand planning can report a goal met.
- Require explicit `VALID_WITHIN_ENVELOPE` quality on every required sustained-campaign phase before offline demand planning can report a goal met; missing or limited evidence remains insufficient.
- Exclude zero or non-finite generator process readings from health summaries, expose invalid sample counts as a measurement-quality limitation, and reject process-counter conversion overflow.
- Report open-arrival scheduler lag sample count, p50/p95/maximum and the monotonic slot-to-admission measurement boundary; label timing accuracy as uncalibrated.
- Add a bounded worker-exhaustion integration check that distinguishes locally dropped arrivals from requests received by an independently counted loopback server.
- Add a bounded cancellation integration check that reconciles scheduled and admitted arrivals, incomplete client journeys, and independently observed server-side completion/cancellation counters.
- Add a fail-fast PowerShell local validation script that runs the full Go test suite and verifies baseline, extended, repeated, and transport-matrix calibration artifacts.
- Add a bounded, local-only HTTP transport fixture matrix (0/1/4/64 KiB, stream, 503, keep-alive/close, private TLS and controlled tail delays) with independent client/server ledgers, schema validation and Linux/Windows CI; distinguish transport checks from native scheduler calibration.
- Compare preflight age and basic runtime/source compatibility offline without pretending OS metadata attests physical host identity; stale or mismatched evidence invalidates apparent fixture equivalence.
- Include distinct, bounded diagnostic signals for generator rate cap, busy workers, missed starts, process CPU pressure, HTTP error responses and unknown bottlenecks without claiming server causality.
- Include separate data-quality findings in HTTP run reports: impossible accounting is INVALID, generator/delivery/preflight uncertainty is LIMITED, and missing independent calibration never yields a false VALID_WITHIN_ENVELOPE claim.
- Add an offline, fail-closed G0-02 reference coverage assessment (`damocline-calibrate --check-scenario ... --calibration ...`), comparing exact measured rates, simple request mix, VUs, duration, protocol and safety ceilings while exposing unverified payload/connection/host evidence. Output is always `INCONCLUSIVE` until stronger validity gates exist and never contacts the target.
- Embed a versioned, sanitized local loopback preflight record in calibration reports with generator/runner identity, UTC test window, fixture fingerprint, per-stage delivered arrivals and the highest *tested passing* rate. It does not certify real application capacity.
- Add validated workload-revision and environment labels to HTTP reports; offline comparisons require matching complete workload conditions and label environment changes as non-causal evidence.
- Add opt-in, environment-backed synthetic data prefixes and a per-journey `{{synthetic_id}}` template value for isolated write workflows; generated identifiers remain out of reports.
- Add an IID-assumption, normal-approximation P95 order-statistic-rank interval when tail sample adequacy permits; preserve its limitations in report schema v6.
- Add an offline `plan` command that compares a calculated business demand target with a complete repeated campaign report, preserving scenario-specific evidence limits and never recommending infrastructure.
- Add mean request latency, sampled in-flight request averages, and a bounded throughput-times-mean-latency consistency signal for Little's Law review.
- Add CI source-build checks for Linux amd64, Windows amd64, macOS amd64, and macOS arm64 without publishing prebuilt binaries.
- Add an explicitly authorized local Docker CPU and memory snapshot command using one bounded `docker stats --no-stream` call.
- Add a loopback-only `dashboard` command for locally exploring an existing HTTP run report without generating traffic or exposing it to the network.
- Add an explicitly authorized, bounded `metrics` command for snapshotting Prometheus-text application metric endpoints without preserving labels, credentials, or making causal bottleneck claims.
- Add optional bounded spike phases and observations up to ten minutes to repeated campaigns, while preserving the global 15-minute budget and existing VU/session safeguards.
- Summarize observed p95, error-rate, and throughput spread across completed campaign observation trials without presenting that empirical range as statistical confidence.
- Add opt-in `window_duration` request metrics and per-window SLO checks. Empty or undersampled windows become inconclusive, while a p95 or error breach in any observed window fails the run.
- Add a bounded `campaign` command with repeated warm-up, observation, and recovery phases. It preserves each phase report, excludes warm-up from approval, and requires observation plus recovery to pass in every trial.
- Add bounded response-driven journey values: extract a JSON Pointer or header value, substitute it in later path segments, headers, or request bodies, and fail safely when extraction is absent or invalid. Extracted values are per-journey and excluded from reports.
- Add bounded weighted workload profiles with named workflow steps, optional profile think times, deterministic closed-loop VU allocation, weighted arrival selection, and per-profile journey evidence in schema-v5 run reports. Profile weights remain declared assumptions until validated against sanitized production usage evidence.
- Add a bounded 30-second loopback soak, 4 KiB response body, extended 2–100 arrivals/s matrix, and scheduler-stall, insufficient-sample, oversize-response, and graceful-drain checks for measurement integrity.
- Permit open-loop arrival rates up to the existing 100 requests/s ceiling, still enforcing `arrival_rate × steps <= max_rps`.
- Stop admission at the configured window, drain admitted journeys for a bounded grace period, mark remaining cancellation as inconclusive, and report the workload window separately from wall-clock elapsed time.
- Count interrupted journeys explicitly, keep their partial durations out of completed-journey percentiles, and prevent trailing think time after the final step from turning a completed journey into a cancellation.
- Add a bounded separate-process loopback reference check for arrival counts, controlled latency, overload drops, and known error recovery. Its smoke result is explicitly not a calibration or capacity certification.
- Upgrade HTTP run reports to schema v4 with sampled Windows/Linux process CPU and resident-memory health and separate workload-window from wall-clock elapsed time; report zero readings explicitly and make goal assessment reject missing process-CPU evidence.
- Document the scientific foundations, assumptions, evidence limits, and validation gates for capacity conclusions.
- Allow `explore` to accept an optional owner demand goal (peak active-equivalent VUs, growth and reserve), schedule an exact workload probe within the configured safety range and print the evidence-limited result immediately.
- Add one-sided 95% statistical error-rate bounds to offline and post-run assessment, with explicit independence/stationarity caveats and zero-failure sample-size illustration. Passing observed thresholds is kept separate from proving a reliability SLO at a stated confidence level.
- Offline business demand goal assessment using recorded `explore` results: peak active-equivalent workload, growth and reserve convert into a target to validate. Require an exact tested level and generator evidence; never claim sustained server capacity from short trials.

- Collect bounded load-generator Go runtime health (sampled heap, goroutines, GC activity and Go CPU runtime occupancy) alongside each HTTP run; explain that these are not operating-system process or server resource measurements.
- Add sampled Windows/Linux generator process CPU and resident-set observations, normalized and reported separately from Go runtime and host metrics. Treat sampling as provisional generator evidence, not a saturation certificate.
- Add deterministic sampling/CPU-occupancy validation tests and ensure the report does not infer a server capacity limit from local generator activity alone.

### Improved
- Enable Up/Down arrow navigation, Enter to select, Esc to exit and number-key shortcuts in supported interactive Windows consoles, with numeric fallback for older hosts and redirected input.
- Replace the thin sword-only terminal splash with the compact, monochrome **Eclipse** Unicode pixel-art mark inspired by Damocline's sword-and-horizon logo. Keep the numbered menu, optional ANSI styling, and explicit traffic consent unchanged.
- Human-readable offline result assessment via `damocline report --file`, optional comparisons and aligned Linux host data; assessment also prints after `run`.
- Explicit distinction between configured RPS ceiling, achieved throughput, request p95, journey p95, tested user levels, and evidence for infrastructure upgrades.
- Safety-oriented caveats for small sample sizes, long-tail responses, shared sessions, dropped arrivals and uncontrolled comparisons.

### Fixed
- Require exact arrival rate, rate ceiling, worker count, duration, timeout, mode, transport and connection policy when matching a workload profile; do not claim offline equivalence for unresolved environment-backed request bodies.
- Classify non-timeout response-body read failures such as truncated responses as transport failures while preserving their HTTP status and incomplete-body observations as separate counters.
- Count only successfully completed response-body reads in `body_count`; truncated and timed-out body reads are represented exclusively by `body_incomplete_count`, preserving phase-counter conservation and quality validation.
- Check source updates before displaying the Eclipse startup splash/menu and avoid a redundant update check after selecting an interactive scenario.
- Guided setup accepts bare domains and HTTPS origins with a trailing slash, and retries invalid addresses instead of aborting.
- Removed third-party load-testing tool references from the interactive welcome banner.

### Added.
- Emit schema-v16 arrival accounting with externally cancelled planned starts separated from scheduler-late and worker-busy drops; preserve missing-versus-zero semantics for historical reports.
- Branded sword splash screen and English numbered home menu for bare `damocline` and `start.cmd`, with safe choices for new, saved, current, help and exit.
- Terminal-aware ANSI color support with plain-text fallback. Non-interactive commands remain unchanged.
- Interactive, opt-in source update notifications with fast-forward Git safety checks, local automatic-update preference, and automatic restart after installing a new version.
- `damocline update` for manual checking and opting in/out; scripted load-testing commands remain unaffected.
- English CLI slash commands `damocline /start` and `damocline /history` to create, list, select and repeat locally saved scenario configurations with renewed traffic authorization.
- Duplicate-aware, private local scenario snapshots with safety checks that reject literal HTTP headers when archiving. No credential values or browser cookies are persisted by the history mechanism.
- Source-based Windows CMD/PowerShell command launcher (`damocline` opens the wizard) and optional POSIX launcher.
- Temporary Chrome/Edge login session borrowing for authenticated read-only HTTP tests; reviewed page suggestions, no credential persistence.
- CI no longer publishes executable release artifacts.
- Initial open-source repository structure, Apache-2.0 license, and contributor guidelines.
- Bounded, single-process, closed-loop HTTP CLI (`run`, `help`, and `version`) with unit tests and CI.
- Architecture, measurement model, design decision, and phased roadmap documents.

- Session-cookie header references from environment variables, response assertions, read/write safeguards, JSON reports, and local examples.

- Native independent journey-arrival scheduling with dropped-start accounting and per-user isolated cookie sessions.\n- Repeated and bracket-refined capacity exploration with finite time budget, per-step thresholds, and conservative inconclusive outcomes.\n- Linux host CPU/RAM sampling and time-window correlation with load trials.\n- Advanced CLI examples and documentation for Windows and Linux.\n\n### Not yet available
- Calibrated server-capacity certification, browser-based journeys, native login/CSRF recorder, per-container and DB telemetry, dashboards, and distributed load generation.
