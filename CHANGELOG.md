# Changelog

This project follows a human-maintained changelog. No release exists yet.

## [Unreleased]

### Added
- Add an offline, fail-closed G0-02 reference coverage assessment (`damocline-calibrate --check-scenario ... --calibration ...`), comparing exact measured rates, simple request mix, VUs, duration, protocol and safety ceilings while exposing unverified payload/connection/host evidence. Output is always `INCONCLUSIVE` until stronger validity gates exist and never contacts the target.
- Embed a versioned, sanitized local loopback preflight record in calibration reports with generator/runner identity, UTC test window, fixture fingerprint, per-stage delivered arrivals and the highest *tested passing* rate. It does not certify real application capacity.
- Add validated workload-revision and environment labels to HTTP reports; offline comparisons require matching complete workload conditions and label environment changes as non-causal evidence.
- Add opt-in, environment-backed synthetic data prefixes and a per-journey `{{synthetic_id}}` template value for isolated write workflows; generated identifiers remain out of reports.
- Add an IID-assumption, normal-approximation P95 order-statistic-rank interval when tail sample adequacy permits; preserve its limitations in the CLI and report schema v6.
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
- Check source updates before displaying the Eclipse startup splash/menu and avoid a redundant update check after selecting an interactive scenario.
- Guided setup accepts bare domains and HTTPS origins with a trailing slash, and retries invalid addresses instead of aborting.
- Removed third-party load-testing tool references from the interactive welcome banner.

### Added
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
