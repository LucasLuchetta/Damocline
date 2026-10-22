# Changelog

## Unreleased

- Correct the capability summary for the phase-aligned campaign integration: document optional Linux process CPU/RSS, typed label-distinct application metrics, exporter-reported process-start observations, and the WordPress fixture's explicitly scoped Docker Engine samples.
- Add a bounded WordPress fixture CPU-quota diagnostic option that records nondefault resource conditions as distinct environment and application revisions.
- Aggregate protocol assessment p95 ranges from complete journey latency, matching the declared SLO, while preserving HTTP request p95 as a separate per-phase diagnostic.
- Allow the bounded WordPress lab verifier to vary closed-loop VUs and its request-rate safety ceiling within explicit limits, enabling prospective exploration and confirmation campaigns with a fixed workflow.
- Make WordPress lab recovery duration configurable within bounded limits so short recovery windows cannot silently under-sample slower workflows.
- Omit reserve-adjusted planning levels and demand margins when candidate confirmations are incomplete, and label predeclared failure levels without implying untested intermediate levels were ruled out.
- Capture allowlisted Apache route timing in the disposable WordPress lab and
  retain per-window server-duration evidence alongside client observations.
  This is diagnostic fixture evidence, not isolated PHP timing or a capacity
  claim.
- Preserve fractional-second campaign start timestamps to improve phase
  alignment while retaining an RFC3339-compatible representation.
- Bucket Apache lab requests by estimated server completion to match client
  response-completion windows; retain any remaining per-window count deltas.
- Capture whole-container CPU and memory from Docker Engine for only the
  isolated WordPress lab project when target cgroups are unavailable to the
  Linux sensor.
- Run the WordPress campaign generator in its own container network namespace
  and reach the target and authenticated sensor over the Compose bridge, while
  documenting that this remains a same-host fixture rather than remote-path
  qualification.
- Protect the bridge-reachable host sensor with a short-lived, locally trusted
  TLS certificate and delete its private key during lab cleanup.
- Record bounded WordPress and database container restart/OOM state and
  lifecycle events from only the isolated Compose project's Docker metadata.
- Capture cgroup-v2 CPU throttling and memory/OOM counter deltas around bounded
  WordPress campaigns when the target container exposes those counters.
- Raise the synthetic WordPress scenario's client request ceiling above its
  observed two-VU demand after the prior campaign flagged `CLIENT_RATE_CEILING_NEAR`.

- Allow the isolated WordPress verifier to use a bounded 0–120 second warm-up when investigating early-window drift.
- Collect a bounded, read-only set of MariaDB status counters and gauges per WordPress campaign phase, requiring complete telemetry and successful status queries.
- Screen campaign drift on complete named endpoint/profile series when available, avoiding aggregate percentiles that mix unlike workflows while retaining aggregate throughput checks.
- Version the named-series stability screen in campaign schema 6 and replay prior campaign schemas with their original aggregate-percentile rule.
- Start the isolated WordPress host sensor after fixture setup so it shares the finalized application network namespace.
- Extend the WordPress fixture warm-up to 30 seconds after named endpoint series showed real early-window latency drift.
- Remove a duplicated invalid overload run from the bounded spike-recovery verifier; keep overload validation in its dedicated script.
- Clarify that offline audit replay generates no traffic, while its saved campaign evidence may contain prior traffic.
- Invoke the explicit `wp` command when initializing and populating the isolated WordPress CLI fixture.
- Avoid PowerShell's reserved `$HOME` variable when checking the WordPress fixture routes.
- Retry bounded WordPress route probes and capture service diagnostics before cleaning an isolated failed run.
- Preserve the random loopback port in WordPress canonical URLs while keeping the in-container generator on port 80.
- Include the WordPress scenario in the isolated Linux generator image used by the application verifier.
- Enable WordPress rewrite rules before probing the REST API in the isolated application fixture.
- Mount explicit read-only Apache rewrite rules in the isolated WordPress fixture so the REST API route resolves inside the generator namespace.
- Extend the bounded WordPress validation observation to 60 seconds after the initial pilot failed the stationarity screen.
- Add an isolated, bounded WordPress and MariaDB Docker lab for repeatable read-only application-workflow validation with an authenticated Linux host sensor and offline audit.
- Require a passing Linux generator preflight before either Docker application campaign begins, and reject invalid or near-saturated generator observations in the WordPress verifier.
- Deduplicate campaign-wide protocol findings so repeated confirmations do not inflate the executive report with the same measurement-quality limitation.
- After a valid observed SLO failure in a bounded spike, retain the failure and run the predeclared base-load recovery phase; do not start another trial.
- Let the offline campaign audit validate exactly that observed spike-failure/recovery sequence while preserving a FAIL verdict.
- Count distinct, safely stopped first-breach campaigns as independent observed failure replications without adding load after the breach; keep repeated passing-candidate confirmations separate.
- Show per-phase host and selected-process CPU plus disk/network evidence in the technical capacity report, keeping those correlations separate from causal claims.
- Include recorded and recomputed run-quality findings by campaign phase in the technical capacity appendix.
- Add `campaign --calibration` to attach one validated generator preflight per phase, recomputing phase workload coverage while retaining target-identity and workload-equivalence limits.
- Align campaign evidence table columns in the protocol Markdown report and label reserve-adjusted arithmetic as conditional rather than an operational capacity recommendation.
- Add reproducible PowerShell lab checks for exporter process restarts, required host-sensor outages, and bounded target overload, with isolated Compose projects and non-overwriting local evidence.

- Summarize exporter-reported process restarts from stable `process_start_time_seconds` series, with explicit unavailable/incomplete coverage and no claim of Docker Engine restart counts.
- Capture selected-process Linux CPU time from `/proc`, use `AT_CLKTCK` to derive per-CPU utilization, and let offline readers recompute each complete sample; preserve incomplete status across PID reuse or counter resets.
- Record bounded Linux host disk and network byte deltas per sensor sample, preserving incomplete coverage when counters reset or visible devices change; report disk, network, RSS, swap, and cgroup evidence independently.
- Extend selected-PID sensor evidence with process CPU time and interval percent, derived from kernel clock ticks and recomputed by offline coverage assessment.
- Preserve distinct labeled application metric series and exporter TYPE/UNIT metadata in schema-v2 metrics snapshots instead of summing by name.
- Allow authenticated application metric collection through a bearer token environment variable; reject plaintext remote token transport.
- Attach bounded application metric snapshots to each closed-loop campaign phase in schema v4 and make incomplete configured collection inconclusive.
- Add an authenticated bounded Linux host sensor and schema-v5 closed-loop campaign phase evidence with shared experiment IDs, clock-offset bounds, and explicit missing-coverage handling.
- Add a controlled Compose lab with a synthetic queue-limited target, application metrics, host sensor, and separate generator configuration.
- Add optional selected-PID cgroup-v2 quota, memory limit/current, and memory-event evidence to the Linux host sensor.
- Add a sealed predeclared confirmation protocol and offline assessment for separate exploratory, passing, and optional failing campaign executions.
- Generate an executive Markdown assessment and local reproduction bundle from the same protocol replay.
- Include per-campaign offline audit, measurement quality, observed SLO spread, telemetry coverage, and cgroup availability in structured protocol assessments; keep safely interrupted outcomes visible without counting them as confirmations.
- Include selected-process RSS and kernel start-time ticks in optional Linux host-sensor samples, independently of whether the target cgroup hierarchy is visible.
- Record host swap total and used bytes in authenticated Linux sensor samples, retaining measured zero swap separately from absent older evidence.

This project follows a human-maintained changelog. No release exists yet.

## [Unreleased]

- Preserve detected workload/application/environment identity changes when other required campaign phases also lack identity fields, and report both facts; apply the same logic in closed-loop audit and open-arrival goal assessment.
- Reject current campaign profiles and calibration artifacts that omit required persisted controls or generator/fixture evidence, while preserving schema-versioned historical calibration fields and explicit zero values.
- Reject campaign artifacts missing required top-level evidence fields; require schema-v5+ campaign timestamps to be valid RFC3339 values while preserving earlier historical timestamp semantics. Apply the same raw-byte checks in audit, collection, dashboard, and redacted export paths.
- Reject campaign trials or phases that omit their recorded number, phase list, outcome, phase name, or embedded run schema/object before zero-value decoding; validate completed and pending trials consistently.
- Reject open-arrival campaign artifacts with omitted schema-defined root, trial, phase, or embedded run fields before goal assessment, dashboard rendering, or redacted export; preserve versioned history and explicit zero values.
- Reject closed-loop and open-arrival exploration artifacts that omit required top-level or per-probe fields before offline goal assessment or redacted export; retain explicit zero values and empty arrays.
- Reject run, campaign, and exploration artifacts with missing or malformed request counters, non-conserved outcomes, or aggregate ledgers that do not reconcile with their steps; apply the check through report, dashboard, goal, and export readers.
- Reject run reports without a valid HTTP(S) origin target identity instead of presenting targetless measurements as attributable evidence.
- Preserve native open-arrival campaign PASS artifacts when phase SLOs pass but measurement quality is insufficient for a capacity decision; offline `goal` now reports `INSUFFICIENT_EVIDENCE` with the quality limitation instead of rejecting the valid campaign. Add a bounded subprocess test using the built CLI and a real loopback campaign; replay produces byte-identical outputs without traffic.
- Include declared environment, workload revision, and application revision in open-arrival campaign goal assessments; missing identity stays unknown and prevents a positive exact-target decision. Record auditor version and executable SHA-256, and pin both source bytes and auditor binary in deterministic offline replay.
- Preserve declared workload revision, application revision, and environment in single-campaign capacity assessments; mark missing or changing phase identity unknown/inconclusive, and require stable matching identity across levels before aggregating a tested boundary.
- Keep campaign capacity Markdown content independent of the report output path so identical source, auditor, and demand inputs reproduce byte-identical reports across destinations; keep local output paths out of the report body.
- Preserve valid partial failure evidence in historical campaign schemas v2 and v4 by validating their recorded outcome/stability vocabulary under historical rules; classify conflicts with current recomputation as inconclusive instead of invalidating the artifact.
- Preserve offline readability for historical closed-loop campaign schemas v2 and v4 with end-to-end JSON fixtures. Compare only fields present in each campaign schema, report later-added journey-throughput and repeat-p95 summaries as unrecorded when absent, and keep historical evidence inconclusive when current quality or stability requirements cannot be recomputed or current SLO checks conflict with historical pass labels.
- Reject JSON artifacts whose object keys repeat after escape decoding across scenario/profile loading, campaign and calibration audit, run inspection, reference acceptance, local history, dashboard, demand assessment, and redacted export. This prevents parsers from deriving different workload controls or evidence from the same bytes.
- Reject campaign phases that embed unsupported HTTP report schema versions during offline audit, instead of interpreting unknown future fields using current semantics.
- Require a nonzero recovery phase for closed-loop campaigns before traffic starts, so a live `PASS` cannot produce evidence that offline capacity audit must reject as missing recovery. Saved profiles with `recovery: "0s"` are rejected rather than silently lengthened.
- Record exact bounded scenario/profile source-file SHA-256 and semantic workload fingerprint in closed-loop campaign schema v15 and offline audit output; reject malformed descriptors or phase-fingerprint mismatches; verify profile byte identity while keeping file contents, paths, and credentials private.
- Advance campaign collection assessment schema to v5 and require recorded generator build/runtime identity and effective CPU entitlement to match across every required phase and tested load level; preserve individual audits while marking missing or changed environment provenance `NOT_COMPARABLE`.
- Clarify that HTTP trace phase counts remain authoritative when a measured duration is below clock resolution, and validate immediate refusals plus delayed transport errors against independent loopback observations and serialized report data.
- Report descriptive p99 adequacy and conditional exact IID 95% order-statistic intervals in schema v32 run evidence, insights, capacity reports, and redacted exports; intervals are bounded from 1,000 to 1,000,000 samples and never imply dependence-aware confidence or a p99 objective.
- Persist schema-v14 repeat-level request-p95 median, median absolute deviation, and a descriptive median +/- 3 MAD band when at least two campaign observation phases completed; recompute from source phases during offline audit and label the band as descriptive under sequential-dependence/stationarity limits.
- Extend local calibration preflight schema v10 and repeated acceptance to reconcile trusted HTTPS reuse at 20 and 40 journey starts/s, retaining exact client handshake and independent server TCP/request/body ledgers and schema-v9 compatibility.
- Advance local calibration preflight to schema v11 and acceptance manifest v7. Record server-accepted TLS connections separately from request-carrying TCP connections so concurrent idle HTTPS dials reconcile against client handshake traces.
- Keep audit comparison aligned with recomputed schema-v14 repeat summaries, and render a single completed observation phase without inventing zero spread or asserting repeat independence.
- Exercise redacted run, campaign, and exploration exports through CLI validation using representative historical schemas; verify nested-field redaction and byte-identical source preservation.
- Bind open-arrival assessment JSON and Markdown reports to the exact campaign source bytes, verify an optional expected SHA-256 before writing, and include that check in the generated offline replay command.
- Record the exact source campaign byte digest beside the semantic evidence ID in closed-loop audit JSON and Markdown reports; verify both before offline replay outputs are written.
- Record the auditor version and exact executable SHA-256 in closed-loop assessments; pin generated replay commands to that executable before reading source campaigns or writing outputs.
- Persist a predeclared objective contract and matching stability screen for open-arrival campaigns; replay phase coverage/delivery policy offline, and include the validated criteria in `goal` output and redacted campaign exports.
- Allow open-arrival campaigns to predeclare the same bounded stability thresholds as closed-loop campaigns, preserving the exact drift policy in offline evidence and making controlled spike/recovery validation resilient to unrelated runner p95 jitter.
- Keep the controlled spike/recovery fixture's native p95/error SLOs strict while widening only its predeclared drift-screen allowance for small ambient runner variation; the product default remains 20% plus 5 ms.
- Keep the profile/replay fixture's loopback throughput below the configured 100 requests/s client ceiling so runner speed cannot turn it into invalid delivery; predeclare its p95 drift allowance while retaining strict native SLOs and production defaults.
- Include the exact source campaign byte digest in open-arrival capacity Markdown reports so copied evidence remains identifiable during offline replay.
- Snapshot predeclared closed-loop campaign objectives in schema v13, validate them before traffic, replay SLO/sample/window/completed-work/drop criteria offline, and include the declared contract in capacity reports.
- Report the highest passing and first failing tested campaign levels explicitly; retain an unknown upper bound when observations are all-pass or non-monotonic.
- Run the repeated 39-profile G0 acceptance lab on Windows and a resource-bounded Linux container in CI for every pull request; archive only its synthetic reports and bounded logs for 14 days to support failure review.
- Persist path-free runner build identity in run report schema v31 and expose per-phase identity and effective CPU consistency in offline campaign capacity reports; changed or missing provenance prevents same-environment claims.

- Reconcile current G0 validation guidance and platform evidence with schema-v9 acceptance after PR #138, including the Linux process CPU allowance and explicit limits of the local fixture results.

- Include the kernel-reported Linux process `Cpus_allowed_list` in each sampled CPU entitlement, detecting effective cgroup-v1/v2 cpuset and post-startup affinity restrictions; unreadable or malformed allowed-CPU evidence keeps capacity quality unknown.
- Test the redacted export allowlist against caller-controlled strings nested anywhere in run, campaign, or exploration artifacts across every supported source schema; fuzz schema and artifact-family selection as well.

- Cap Linux generator CPU entitlement by readable cgroup v2 effective cpusets as well as ancestor quotas; expose the combined source in existing CPU capacity metadata, fail closed on malformed present cpusets, and validate synthetic cpuset restrictions and changes.

- Add versioned private local campaign profiles with migration, explicit replacement/deletion, repeatable run controls, fresh execution opt-ins, and automatic complete campaign reports under `.damocline/reports/`. Reject literal credentials in both legacy and versioned workflow headers, and validate the published profile example against the current scenario schema.
- Add offline `campaign profile verify` to compare saved workload and campaign controls against persisted phase configurations, with explicit mismatch/unverifiable states and an optional non-overwriting JSON result.
- Add replayable executive and technical Markdown campaign reports from the same offline assessment, and persist peak-demand, growth, and reserve inputs in assessment JSON.
- Pin generated assessment replay commands to the SHA-256 evidence ID of each campaign source and reject changed evidence before writing outputs.

- Define nearest-rank latency percentiles and replace the approximate p95 rank interval with an exact binomial order-statistic interval; persist the estimator, confidence method, and IID/stationarity assumption in schema v30.

- Add an exact, assumption-labeled repeat-trial pass probability interval to offline campaign audits; aborted and incomplete trials remain excluded.

- Add bounded Go fuzz smoke checks for request-template interpolation, escaped-query byte accounting, and expanded URL size/origin invariants.

- Bound base origins, path templates, and expanded HTTP request URLs, headers, and bodies; stop expansion at intermediate/output limits, record local rejections in schema v29, and invalidate delivery attribution when a generated request never reaches the target.

- Add an optional per-host connection cap and schema-v28 transport acquisition diagnostics. A delayed reused-connection acquisition is surfaced as a client-side queue symptom without claiming proven pool exhaustion.

- Add explicit scenario version 1 with named workflows and a non-overwriting `migrate-scenario` command that converts legacy flat steps or weighted profiles while preserving the source file.

- Validate configured session cookies as non-empty, uniquely named `name=value` sets before counting them as independent workflow sessions; cookie values remain in-memory only.

- Recompute closed-loop campaign observation spread from saved phases during offline audit, expose per-trial request/journey SLO and useful-throughput evidence, and reject a persisted spread that does not reconcile with its source observations.
- Add optional per-workflow/per-actor authentication cookie sessions for weighted journeys in both closed-loop and open-arrival runs; keep credentials and environment-variable names out of reports and workload fingerprints, and persist only the selected workflow session scopes in report schema v27.
- Advance calibration evidence to schema v9 with exact 1/2/5/10/20/50-VU closed-loop profiles, and acceptance manifest v5 with per-profile iteration and independent server-ledger reconciliation. Exact closed-loop campaign preflight can now pass for matching short local generator profiles; other VU levels and controls remain blocked.
- Add opt-in closed-loop campaign calibration preflight, persist exact VU-level coverage and required-policy metadata in campaign schema v12, and block before execution when requested coverage is unavailable. Arrival-mode calibration profiles do not qualify closed-loop VUs.
- Select weighted workflows at every journey start in both closed-loop and open-arrival runs. Persist schema-v26 profile selection semantics, per-profile selected/start/drop counters and observed actors, validate their conservation, and keep historical actor-assignment reports readable.

- Add an optional bounded `--stabilization` stage before campaign observation in closed-loop and open-arrival modes; persist schema v11/v7 evidence, screen it with the saved drift policy, budget its drain, and validate it during offline audit.

- Add explicit synthetic-ID-scoped cleanup steps that run after business assertion failures and within the reserved drain window after external cancellation; identify cleanup steps in run report schema v24.
- Add opt-in exact local generator calibration gates to `run` and `arrival-campaign`; block before traffic when calibration is missing, stale, runtime-mismatched, or outside the exact workload matrix, while preserving unknown target identity and sustained headroom.
- Restrict calibrated `explore-arrival` searches to exact compatible matrix rates, cap the effective ceiling at the highest covered rate, retain requested/effective bounds and qualified rates in schema v2, and validate the qualified set offline.
- Extend calibration preflight to schema v8 with three trusted HTTPS GET profiles at the exact 2/5/10 arrivals/s rates; reconcile native-engine TLS handshakes, client bodies, and independent loopback request/body/TCP ledgers while preserving v1-v7 matrices.
- Add optional bounded Prometheus-text sampling during runs and both campaign modes; retain per-phase timestamps and partial collection status in run schema v25 without folding auxiliary scrapes into load counters.
- Extend the loopback-only report dashboard to render bounded application-metric observations and saved closed-loop/open-arrival campaign phases, preserving each workload unit and pending-checkpoint state.
- Overlay HTTP-window p95 points and external metric observations per phase on a shared UTC axis with independent scales; retain subsecond start-time precision in new run reports.
- Add opt-in `report export` for allowlisted, anonymized run, campaign, and exploration summaries; omit destinations, private names/free text, absolute timestamps, and custom metric names/values.

### Fixed
- Reject report histories that exceed the 100-report or 512 MiB safety envelope before reading archive bodies; bound each descriptor read independently to 16 MiB, including after metadata inspection.
- Enforce the configured observation boundary for scheduler arrivals and closed-loop journey starts even when timer delivery is delayed; late open-loop slots remain explicitly reconciled as generator drops while admitted work drains.

### Added
- Add a bounded local report history with immutable timestamp/content-digest IDs, list/save commands, and explicit delete/prune operations; stop archival at 100 reports or 512 MiB instead of deleting evidence automatically.
- Checkpoint closed-loop and open-arrival campaign JSON atomically after each completed phase, preserve a pending trial as `RUNNING`, and audit interrupted artifacts as incomplete evidence.
- Add a bounded 0–5 minute inter-trial cooldown to both campaign modes, persist unique random trial IDs, include pauses in campaign budgets, validate schedules offline, and retain cooldown interruptions as aborted trials.
- Require `--out` for every non-dry-run closed-loop or open-arrival campaign so failed, incomplete and passing phase evidence is retained for offline review.
- Add an integrated loopback saturation/recovery fixture for closed-loop and open-arrival campaigns, reconciling phase SLO outcomes, planned and completed workload, unfinished journeys, and generator samples.
- Preserve the recovery probe after an observed spike SLO failure in closed-loop and open-arrival campaigns, keep the trial outcome FAIL, and require that evidence during offline audit.
- Add optional consecutive-sample CPU and RSS abort guardrails for the local generator to closed-loop and open-arrival campaigns; persist the policy and trigger observation in run and campaign evidence and validate it during offline assessment.
- Add an explicit staging long-run mode to closed-loop and open-arrival campaigns: 30-minute observation/soak phases, a required operator-supplied budget capped at two hours, retained JSON output, and a 120-window limit. Existing default run and campaign ceilings remain unchanged.
- Add a concurrent loopback fixture that cross-checks each actor's cookie against the response-derived ID used by that actor's next request.
- Mark every non-GET/HEAD route as `MUTATING` in CLI previews, prove synthetic IDs are unique per journey, and test that configured cleanup still runs after a failed business postcondition.
- Extend loopback coverage to prove per-journey synthetic write IDs are unique and explicit cleanup steps run after business postcondition failures; document when cancellation or cleanup failure can leave synthetic data behind.
- Support bounded JSON Pointer scalar postconditions on each workflow step, classify false HTTP-200 business outcomes as failures, include assertions in workload fingerprints, and keep assertion values out of reports.
- Allow same-journey extracted values in query parameter values with query-component escaping, malformed-template rejection, and loopback coverage proving special characters cannot add parameters or change the request path.
- Preserve probe-level reasons in exploration schema v2 and CLI output; current-schema accounting failures, invalid quality, dropped arrivals and cancelled HTTP/business journeys stop a VU level as INCONCLUSIVE instead of counting it as application PASS/FAIL. Share conservative generator evidence checks between exploration and exact business-goal assessment so generator-bound failures cannot form an application-capacity bracket.
- Add optional final campaign soak (`--soak`, 10s–3m) after all independent trials; include its drain allowance in the 15-minute wall-time cap, persist schema-v4 stage evidence, validate it offline, and screen full journey/window SLOs and coarse drift. This remains bounded local campaign evidence, not a capacity certification.
- Classify current-schema campaign PASS/FAIL phases as invalid delivery when generator health, sustained CPU headroom, or configured client-rate headroom is inadequate; use mean process CPU so an isolated diagnostic peak does not reject a phase.
- Persist bounded configurable campaign drift-screen policies in campaign schema v5, reject policies that cannot be supported by the declared observation windows, and recompute the exact policy during offline audit and cross-level comparability checks.
- Add bounded repeated open-arrival campaigns with warm-up, sustained observation, optional spike, recovery and final soak; retain every phase report and support offline exact scheduled-start demand assessment.
- Calculate an optional peak, growth, and reserve demand in scheduled journey starts/s during `explore-arrival`, and add an offline `goal` assessment that requires the exact recorded rate and keeps LIMITED short-trial evidence inconclusive.
- Add bounded `explore-arrival` journey-start-rate exploration, separate from VU exploration, with repeated passing probes, optional exact target, preserved native run evidence, campaign-aligned evidence classifications, and inconclusive handling for underdelivery or generator/client limits.
- Add a reserved-origin read-only arrival-mode example and document the distinct units and schema-v1 exploration artifact.
- Write JSON reports through restricted same-directory temporary files and replace destinations only after serialization, byte-limit validation, file sync, and successful close; sync the parent directory on Unix and use write-through replacement on Windows.
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
- Require explicitly selected Docker container IDs or names for the authorized CPU/memory snapshot, bound selections to 64, and redact runtime names from saved telemetry.
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
- Validate open-arrival exploration coverage against weighted-profile steps and each profile's saved observation-window evidence.
- Mark an exact open-arrival target as tested whenever its probe is recorded, including failures or interruption before a run, so offline goal assessment can reassess the available evidence.
- Validate campaign window coverage before load generation, bound default windows across observation/recovery/soak phases, and keep inadequate generator or measurement evidence from becoming an offline SLO-failure finding.
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
- Include bounded per-phase application metric value ranges in protocol technical reports while omitting exporter sources and label values.
- Add relative-path restoration and portable offline replay instructions to protocol reproduction bundles.
- Compare synthetic-target completion/rejection counter deltas with independently observed HTTP 200/503 counts, checking resets and preserving unsampled boundary differences.
- Separate candidate-level and predeclared-failure p95/error ranges in capacity assessments instead of presenting different load levels as one repeatability range; retain zero-error minima.
- Link each campaign, phase, and measurement-quality row in protocol Markdown directly to its campaign evidence digest.
- Report declared peak demand, growth, reserve, and signed conditional arithmetic margin separately from any operational demand margin.
