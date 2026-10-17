# Guided terminal setup

Damocline is a standalone native **Go CLI**. No Docker, Node.js, Python, hosted account, browser dashboard, or k6 installation is required.

## Start

Download the `damocline-preview-binaries` artifact from a successful [CI run](https://github.com/LucasLuchetta/Damocline/actions/workflows/ci.yml). Extract the ZIP and open its `windows-amd64` folder. It contains `damocline.exe` ready to execute, without Go, Python, Node.js, Docker, or k6.

In Windows PowerShell or CMD, from that folder:

```powershell
.\damocline.exe start
```

When the executable is available on your PATH, simply type **damocline start**.

On Linux/macOS, use **./damocline start**. You can also build from source with Go 1.24+:

```powershell
go build -o damocline.exe ./cmd/damocline
.\damocline.exe start
```

The wizard guides you through the HTTPS application origin, read-only page paths, optional expected response text, authentication mode, and load settings. Choose a single fixed-level run or repeated virtual-user capacity exploration.

The wizard does not touch Docker or your target server. The **native HTTP load generator runs on the machine where you execute start**, normally your development laptop.

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
.\damocline.exe start
Remove-Item Env:\DAMOCLINE_SESSION_COOKIE
```

For independent test accounts, set **DAMOCLINE_VU_COOKIE_1** through **DAMOCLINE_VU_COOKIE_N** for the maximum number of configured VUs. Every user should have a different authorized test session. A single cookie reused by 10 VUs is not 10 distinct logged-in people.

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
