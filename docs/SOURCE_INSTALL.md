# Source-based installation and temporary browser login

Damocline is an independent Go load-testing CLI. It is **not a k6 wrapper**.

## Clone once, type `damocline`

On Windows install Go 1.24+ and Git, then run from PowerShell:

```powershell
git clone https://github.com/LucasLuchetta/Damocline.git
cd Damocline
.\setup.cmd
```

The setup command creates `%LOCALAPPDATA%\Damocline\bin\damocline.cmd` and adds that directory to your **user PATH**. It does not ask for administrator rights or download a release executable. Open a **new terminal** and type:

```powershell
damocline
```

It opens the guided wizard automatically. `damocline start` works too, as do `damocline run` and `damocline explore`.

Without modifying PATH, start from your clone using `.\start.cmd` or `go run ./cmd/damocline start`.

**Go still builds a temporary native executable to run source code.** The Windows security system may inspect that process; this is not a technique to bypass antivirus warnings. No unsigned `.exe` is distributed through CI.

Update later with `git pull` in the cloned folder. The registered command uses that checkout. If you move the folder, rerun `setup.cmd`.

On Linux/macOS run `sh scripts/install.sh`, add `~/.local/bin` to PATH if needed, and type `damocline`. A graphical Edge/Chrome installation is needed only for browser-assisted login.

## Sign in without pasting credentials

In the wizard choose **Authentication mode 4: temporary browser login**.

1. Enter the **HTTPS origin** of an application you own or are authorized to test.
2. Give its login path (usually `/login`) and initial read-only page paths.
3. Damocline starts a **new isolated Edge/Chrome profile**, opening the login page. Authenticate yourself normally. The CLI never asks for your password.
4. Navigate to the pages you want to inspect and press Enter in the terminal.
5. Damocline reads session cookies **only for the target host** and borrows them in process memory. It suggests some visited paths; **each additional GET requires explicit approval**. This is *not* a recording of form submissions or the login POST.
6. The temporary browser is closed, and the tool attempts to delete its profile; the saved scenario contains `${DAMOCLINE_SESSION_COOKIE}` as a variable reference, **not the secret**.
7. Review the workload, explicitly type `AUTHORIZE`, then confirm before any HTTP load begins.

The next run reuses the scenario but opens the temporary browser **again** to acquire a new session. Browser navigation is **not** fully replayed. Some applications need CSRF tokens, additional headers, a JavaScript client or WebSockets; borrowed cookies may not be sufficient. A redirect to login is treated as a failure by default.

**One captured login is one account.** Ten virtual users sharing that session are not ten distinct human identities. For reliable independent-user studies, configure different authorized test accounts through the per-VU session mode.

## Safety and limitations

- Start against staging if possible. Only test authorized systems.
- Authentication cookies and passwords must never be placed in issues or committed scenarios. `.damocline/` is gitignored.
- The isolated debugging endpoint uses loopback `127.0.0.1`; avoid running this browser flow on untrusted shared devices.
- Temporary browser profile removal can fail if the browser or Windows locks files. Do not use a sensitive production admin account for exploratory tests. Manually remove abandoned `damocline-browser-*` temporary folders if needed.
- The native motor generates HTTP traffic from **your development machine**, not the VPS. Linux host metrics still require the optional `monitor` command on the remote host.
- Capacity findings are scenario-specific passing levels, not a universal server maximum.

## Automatic source updates

The installed source launcher checks for a newer `origin/master` when you start Damocline interactively. It offers to fast-forward and restart, or you can opt in to future automatic updates with `damocline update --auto`. Changes in your checkout and feature branches are never overwritten. See [Source updates](UPDATES.md).
