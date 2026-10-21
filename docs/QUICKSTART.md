# Quick start — HTTP MVP

This release is an **experimental, self-hosted load generator**. It sends HTTP requests, not full browser events. It can exercise authenticated pages and APIs, but it does not automatically measure JavaScript rendering or find a universal server capacity.

## 1. Install / build

**Option A — build from source (Windows PowerShell, Linux, or macOS):** install Go 1.24+, clone the repository, and run:

```powershell
git clone https://github.com/LucasLuchetta/Damocline.git
cd Damocline
go test ./...
go build -o damocline.exe ./cmd/damocline
.\damocline.exe help
```

If you are using the `feat/http-mvp` branch before merge, run `git switch feat/http-mvp` after cloning.

**Option B — Windows executable:** download the `damocline-preview-binaries` artifact from a successful [GitHub Actions run](https://github.com/LucasLuchetta/Damocline/actions/workflows/ci.yml), unzip it, and use `damocline-windows-amd64.exe`. These are temporary CI artifacts, not signed official releases.

## 2. Try localhost first

From the repository root, open a terminal with a local static server:

```powershell
python -m http.server 18080 --bind 127.0.0.1
```

Open another terminal:

```powershell
.\damocline.exe run --config examples/local-smoke.json --dry-run
.\damocline.exe run --config examples/local-smoke.json --authorized --out reports/local.json
```

`--dry-run` resolves the scenario but sends **zero requests**. `--authorized` is required for real traffic. The sample uses 2 VUs, a 5 RPS cap and only GET.

## 3. Safely configure an authenticated web app

Copy `examples/webapp-readonly.json` to `myapp.local.json` (the `*.local.json` pattern is gitignored).

Set `base_url` to your application's HTTPS **origin**, for example `https://app.example.com`. Replace the sample `/dashboard` and `/employees` paths with real routes you own and are authorized to test. Include an unmistakable `must_contain` marker for your authenticated page; an HTTP 200 might otherwise be a login page.

### Windows PowerShell: session cookie without committing it

Open your own authenticated browser session and securely obtain only the necessary `Cookie` header value. **Do not share the cookie with ChatGPT, paste it into an issue/PR, or add it to JSON.** Enter it locally:

```powershell
$cookie = Read-Host "Cookie header value from your authorized session" -AsSecureString
$env:DAMOCLINE_SESSION_COOKIE = [System.Net.NetworkCredential]::new("", $cookie).Password
Remove-Variable cookie
```

The JSON uses `"Cookie": "${DAMOCLINE_SESSION_COOKIE}"`; the CLI resolves it from your current process environment, never prints the value, and does not add it to reports.

Then:

```powershell
.\damocline.exe run --config .\myapp.local.json --dry-run
.\damocline.exe run --config .\myapp.local.json --authorized --out reports/myapp-smoke.json
Remove-Item Env:\DAMOCLINE_SESSION_COOKIE
```

A bearer token can be supplied in a similar way with `"Authorization": "${DAMOCLINE_AUTHORIZATION}"`. Include the `Bearer ` prefix in the environment variable value if required by your API.

**Important:** a session cookie may be tied to a single user. Multiple Damocline VUs reusing one cookie are not equivalent to distinct real authenticated accounts. Use test users and a staging environment when possible. Pages may poll or trigger side effects even with GET; confirm each route's behavior first.

### Starting production testing

Start with **2 virtual users / 5 RPS / 30 seconds** on read-only pages. Run during a quiet maintenance window, coordinate with the operator, check server health, and raise limits gradually only if measurements remain stable. Prefer staging with realistic data. The engine has hard configuration ceilings of 50 VUs, 100 RPS and 3 minutes but those ceilings are not safe recommendations for every app.

## 4. Read the results

A run prints:

- Total requests, successful operations, failure reasons and cancelled requests.
- Per-step p95 latency and aggregate p50/p95/p99.
- Achieved throughput, configured virtual users, and whether your p95 and error-rate thresholds passed.
- For open-loop arrival mode, scheduled/started/completed/dropped arrivals and scheduler-lag summaries; plus sampled Go-runtime and supported OS process health for the generator.
- A JSON artifact for comparing later runs.

**PASS means the declared objectives passed in this one run.** It does not mean the server supports that many *people* indefinitely or that it has a known maximum capacity. The report's error-rate upper bound assumes independent stationary outcomes. Current schema-v32 reports also show exact p95 and p99 order-statistic rank intervals when they have at least 200 and 1,000 observations, respectively, up to a one-million-sample analysis limit. These intervals assume independent, stationary latency observations; they do not test those assumptions or account for temporal or actor-level dependence. When an interval is unavailable, the percentile remains descriptive. Generator health samples the Go runtime and, on Windows/Linux, OS process CPU/RSS; sampling can miss brief saturation. The run does not separate warm-up from a steady-state observation window.

To compare traffic under different settings, copy the scenario, change `users` deliberately, keep route/data/latency/error definitions constant, and repeat in comparable conditions. Results from overlapping pages cannot be added together to produce a system-wide limit.

For Linux or Docker servers you administer, collect server-side signals separately (for example CPU and memory using `docker stats`) while running Damocline from another machine. Host monitoring is not integrated into the MVP yet.

## 5. Limitations and safeguards

- **Execution model:** closed-loop VUs and open-loop journey arrivals are supported. Independent session arrivals with dependent within-session steps (a partly open model) are not implemented.
- **Protocol:** HTTP/HTTPS only, no browser DOM, WebSockets, or JS rendering.
- **Login:** no built-in browser/form login recorder. Use environment-backed headers, and confirm redirects to login are treated as failures by default.
- **Body size:** responses larger than 2 MiB are flagged, not silently benchmarked as successful.
- **Traffic bounds:** up to 50 VUs, 100 RPS, 3 minutes, 20 steps, 30-second request timeout. Use much smaller values when beginning.
- **Writes:** GET/HEAD are available by default; POST/PUT/PATCH/DELETE require explicit `--allow-writes` and safe test data.
- **Security:** no HTTP redirects are followed; non-local plaintext HTTP requires `--allow-insecure-http`. These features do **not** prove you own a target; follow explicit authorization and rate-limit policies.
- **Reporting:** no response bodies, cookies, or authorization headers are exported. Journey metrics cover completed journeys; shutdown does not yet provide a full drain and latency accounting for every unfinished journey. Treat scenario paths and test environment details as sensitive where appropriate.

See [Measurement model](METRICS.md) for precise terminology and the [Roadmap](ROADMAP.md) for future capacity exploration and host telemetry.
