# Guided terminal setup

Damocline is a standalone native **Go CLI**. No Docker, Node.js, Python, hosted account, browser dashboard, installation is required.

## Start from cloned source (Windows)

Install [Go 1.24+](https://go.dev/dl/) and Git, then:

```powershell
git clone https://github.com/LucasLuchetta/Damocline.git
cd Damocline
.\setup.cmd
```

Open a new terminal and type **`damocline`**. The installer adds a small source-based `damocline.cmd` shim to your user PATH. No downloaded unsigned `.exe` is distributed. Internally, `go run` still compiles and runs local Go code. To skip PATH setup, use `.\start.cmd` from the repo root.

The guided CLI requests the application origin, authorized GET pages, authentication and workload limits. **Authentication option 4 opens a temporary Edge/Chrome window**. Sign in normally, visit read-only pages, return to the terminal and press Enter. Damocline borrows target-scoped cookies only for this run. You may opt in to saving suggested visited paths as read-only GETs. It never saves a password or login form data.

See [source install and browser security](SOURCE_INSTALL.md).

## Terminal welcome screen

Run `damocline` (or `.\\start.cmd`) to open the branded sword splash and main menu. The screen is deliberately small and line-oriented so it works in PowerShell, Windows Terminal, CMD, Unix terminals and redirected input without any additional terminal UI dependencies.

```text
             ▄▄▄▄▄▄▄▄▄▄▄▄▄
         ▄███████████████████▄
       ▄██████████▀█▀██████████▄
      ███████████  █  ███████████
      ██████████  ▄█▄  ██████████
      ██████████  ▀█▀  ██████████
      ▀▀▀▀▀▀▀▀▀▀   █   ▀▀▀▀▀▀▀▀▀▀

         ▄████████ █ ████████▄
           ▀██████ █ ██████▀
              ▀███ █ ███▀
                 ▀ █ ▀
                   ▼

  D A M O C L I N E
  Find the limit before it finds you.
  LOCAL-FIRST HTTP CAPACITY EXPLORER

  ─────────────────────────────────────────────

  What would you like to do?

  > [1] New load test        Create a new scenario
    [2] Saved scenarios      Select a previous configuration
    [3] Continue current     Reuse the active scenario
    [4] Help                 Commands and documentation
    [0] Exit                 Leave without generating traffic
```

On supported interactive Windows consoles (PowerShell, Windows Terminal and CMD), use **Up/Down arrows** to move the highlight, **Enter** to select, **Esc** to exit, or a number key for quick selection. On redirected input, unsupported console hosts and Unix shells, type a number and press Enter; default is `1`. Option 3 explains when no current scenario exists. Option 4 displays help and returns to the menu. Colors are optional and are disabled for non-terminal output, `NO_COLOR`, `TERM=dumb`, and non-ANSI Windows hosts. The original **Eclipse** illustration adapts Damocline's sword-and-horizon branding with Unicode block characters (`█`, `▄`, `▀`) rather than copying another tool's logo. A terminal font supporting Unicode block elements (for example Cascadia Mono) is recommended for the intended pixel-art appearance.

The welcome screen does not run a benchmark, check a target, collect credentials or bypass any existing traffic authorization. `/start`, `/history`, `run`, `explore` and all other explicit subcommands remain direct, script-friendly CLI commands. The interactive Windows menu reads native console key events and temporarily enables ANSI cursor movement to redraw its highlight; it restores console modes on exit. It does not use full-screen terminal control. Numbered input remains available when the console does not support that capability.

## Startup source-update check

Bare `damocline` checks for source updates **before** drawing the Eclipse artwork and main menu. On an eligible clean `master` checkout, it offers a safe fast-forward update or applies it when automatic updates have been enabled, then restarts into the updated CLI. If the update is skipped, unavailable offline, or there is no newer commit, the original startup continues normally. Choosing a scenario from the home menu does not repeat the Git check. Direct guided entrypoints (`start`, `/start`, `/history`) continue to check on their own; scripted benchmarks do not. See [source updates](UPDATES.md).

## Slash commands and scenario history

Use these **English** commands from PowerShell/CMD after installing the source launcher:

```powershell
damocline /start    # always create a new scenario
damocline /history  # select and repeat a saved scenario
damocline           # use current settings, start a new scenario, or open history
```

These are CLI arguments, not HTTP routes. When you open plain `damocline` and a saved profile exists, you may enter `/start` or `/history` at the profile prompt. The original `damocline start` interactive wizard continues to work.

History is **local to the launcher checkout's `.damocline` directory**, not global across different clones. On Windows the registered launcher switches to the source checkout before starting the program. Each distinct scenario is kept once and can be selected by number; switching scenarios preserves the current settings in history. Your original `.damocline/scenario.local.json` remains the active profile for compatibility. The history entries are configuration snapshots, not historical benchmark results; JSON run reports remain in `.damocline/reports/`.

A selected scenario is **not executed automatically**. It still shows the plan, requires typing `AUTHORIZE` each time and a separate `y` to start. Browser-login mode opens a fresh temporary browser for manual login again; no session cookie is persisted. A scenario edited to include literal HTTP header values cannot be archived until those headers are changed to environment-variable references such as `${DAMOCLINE_SESSION_COOKIE}`.

## Saved settings

First use creates private local files in the source checkout's working directory (normally the launcher checkout):

- **.damocline/scenario.local.json** — sanitized reusable scenario.
- **.damocline/start.json** — guided mode settings.
- **.damocline/reports/** — JSON reports after completed runs.
- **.damocline/history/** — snapshots of previously replaced scenarios, without resolved secrets.

The .damocline folder is gitignored by the project. No cookie values are saved. When you type **damocline** again, press Enter to reuse the current profile, or choose `/start` or `/history` to create or select another scenario.

## Authentication

Choose **public** for unauthenticated pages. For a shared existing session, set the environment variable DAMOCLINE_SESSION_COOKIE locally before running the wizard. In PowerShell:

```powershell
$cookie = Read-Host 'Test session Cookie header' -AsSecureString
$env:DAMOCLINE_SESSION_COOKIE = [System.Net.NetworkCredential]::new('', $cookie).Password
Remove-Variable cookie
damocline
Remove-Item Env:\DAMOCLINE_SESSION_COOKIE
```

**Temporary browser login:** choose option 4, log in manually in the isolated browser and confirm which visited pages should be GET-tested. The session cookie is kept only in RAM and is not saved. You will log in again on the next run. Multiple VUs using this one cookie are not distinct accounts.\n\nFor independent test accounts, set **DAMOCLINE_VU_COOKIE_1** through **DAMOCLINE_VU_COOKIE_N** for the maximum number of configured VUs. Every user should have a different authorized test session. A single cookie reused by 10 VUs is not 10 distinct logged-in people.

Do not paste session cookies into source files, issues, or chats. If an environment variable is missing, setup is saved but **no requests are sent**; configure it and run "start" again.

## Safety and behavior

- The wizard only creates **GET** steps. GET endpoints can still have side effects, so review the target.
- Only authorized HTTPS sites (or localhost/loopback HTTP) are accepted.
- Conservative defaults: 2 initial VUs, 20 requests/second global maximum, p95 objective 1.5 seconds, 1% error budget.
- Before any traffic, the wizard shows the plan, requires typing **AUTHORIZE**, and separately asks whether to start. Authorization is not saved between runs.
- Tests execute using the same Go engine as the advanced **run** and **explore** CLI commands; "start" is a guided frontend, **the native Damocline engine**.
- Reports show scenario-specific observations, not the universal server capacity or guaranteed number of real people who can log in.
- Linux CPU/RAM collection currently requires the separate "damocline monitor" command on the VPS. One-command remote SSH monitoring remains planned, not implemented.

For automated workloads, skip the wizard and use **damocline run --config ...** or **damocline explore --config ...** with explicit authorization flags.

## Application addresses

The wizard accepts a plain hostname or an origin with or without a trailing slash. All three inputs below are equivalent:

```text
app.example.com
https://app.example.com
https://app.example.com/
```

It normalizes them to `https://app.example.com` before writing the scenario. If an address is invalid, the wizard **asks again** rather than terminating. Enter page paths separately in the next step. HTTPS is required except for localhost and loopback targets. Credentials in URLs, query parameters, and fragments are rejected.

