# Source updates

Damocline is installed from a Git clone and launched using the Go toolchain. No precompiled Windows executable is downloaded by Damocline. Go compiles source locally as needed; Windows security software may still inspect that local process.

## Opening Damocline

When you type `damocline` or `damocline start` from a clean `master` checkout, the interactive CLI checks `origin/master` for a newer commit.

```text
Damocline update available: a1b2c3d4 -> e5f6a7b8 (origin/master)
Update and restart? (y=once, a=always, n=skip) [y]:
```

- **y** installs this update and relaunches the new CLI.
- **a** enables automatic future updates for this local checkout, installs the current update, and relaunches the new CLI.
- **n** continues with the current version.

Automatic updates are **off by default**. The preference is kept in the local, gitignored `.damocline/updates.json` file.

## Manual commands

```powershell
damocline update --check
damocline update
damocline update --auto
damocline update --off
```

The update operation uses `git fetch` and a guarded `git merge --ff-only` against the configured `origin/master`. It **never force-resets**, overwrites tracked edits, updates unrelated branches or fetches third-party release executables. Offline checks fail open, allowing the current version to run.

When working on a feature branch, switch to `master` first:

```powershell
cd $HOME\Damocline
git switch master
damocline
```

Scripted `run`, `explore`, `monitor` and `correlate` commands do not check or change the Git checkout, keeping load-test automation predictable.

**Security:** Git ancestry checks help prevent accidental conflict, but do not cryptographically attest commit authorship. Automatic updates execute code from the repository's configured Git remote. Only enable unattended updates if you trust that remote and its maintainers. Never put real application addresses, test session secrets, or private benchmarks into public source control.
