# ADR 0003 — Lightweight terminal welcome screen

- **Date:** 2026-10-17
- **Status:** Proposed

## Context

The initial CLI opens directly into a long sequence of prompts. New users need a
recognizable product identity and a clear map of the supported testing flows.
The proposed home screen should work in Windows PowerShell/CMD and Unix without
changing the HTTP engine, requiring a graphical interface, or downloading a
full-screen terminal UI dependency.

## Decision

- For the no-argument `damocline` entrypoint, render an original sword motif
  with a compact English numbered menu and choices for new, saved, current, help
  and exit. `start.cmd` forwards CLI arguments and defaults to that entrypoint.
- Route each choice through the existing interactive scenario flows; reuse the
  same local settings, fresh authentication steps and explicit traffic consent.
- Keep `damocline /start`, `damocline /history`, `damocline start`, and
  non-interactive `run`, `explore`, `monitor`, etc. fully compatible.
- Use line-oriented, numeric input and Go's standard library. A full-screen
  cursor/key manager is not required for this first version.
- Apply restrained ANSI color only for supported interactive terminals. Disable
  control sequences for piped output, `NO_COLOR` and `TERM=dumb`; Windows
  support is enabled only for known ANSI-capable hosts. No screen clearing,
  remote content, animation, or other terminal state changes.

## Consequences

**Follow-up (2026-10-17):** The home menu now uses native Windows console key events and temporary VT output to support Up/Down, Enter, Escape and numeric shortcuts. The terminal mode is restored when the user leaves the menu. If console setup fails or input is redirected, the original line-oriented numbered prompt remains available. The safe, interactive source-update check runs before rendering the home screen, and the choice handler does not check again; direct guided commands still check before setup. No third-party terminal dependency is introduced.

The UI works without third-party dependencies and can be tested deterministically
using `io.Reader`/`io.Writer` fixtures. Users choose with a number and Enter,
not arrow keys. Existing scenarios and reports remain local. Showing the menu
does not itself send application traffic or grant permission to start a test.

This work builds on ADR 0002's local scenario history and must land after the
scenario history PR or be reconciled before merging to the default branch.
