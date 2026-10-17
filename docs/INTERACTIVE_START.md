# Guided terminal setup

Damocline is a standalone native **Go CLI**. No Docker, Node.js, Python, hosted account, browser dashboard, or k6 installation is required.

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

## Saved settings

First use creates private local files in the current directory:

- **.damocline/scenario.local.json** — sanitized reusable scenario.
- **.damocline/start.json** — guided mode settings.
- **.damocline/reports/** — JSON reports after completed runs.

The .damocline folder is gitignored by the project. No cookie values are saved. When you type **damocline start** again from that directory, press Enter to use the saved profile, or answer "n" to create a new one.

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
- Tests execute using the same Go engine as the advanced **run** and **explore** CLI commands; "start" is a guided frontend, **not a k6 wrapper**.
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

