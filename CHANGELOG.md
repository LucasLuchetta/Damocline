# Changelog

This project follows a human-maintained changelog. No release exists yet.

## [Unreleased]

### Added
- Add one-sided 95% statistical error-rate bounds to offline and post-run assessment, with explicit independence/stationarity caveats and zero-failure sample-size illustration. Passing observed thresholds is kept separate from proving a reliability SLO at a stated confidence level.

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
