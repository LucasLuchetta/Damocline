# Changelog

This project follows a human-maintained changelog. No release exists yet.

## [Unreleased]

### Fixed
- Guided setup accepts bare domains and HTTPS origins with a trailing slash, and retries invalid addresses instead of aborting.
- Removed third-party load-testing tool references from the interactive welcome banner.

### Added
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
