# Quick start — run a local HTTP test

Damocline is an experimental, local-first HTTP capacity tool. This guide gets
the source version running against a synthetic server on your own computer.
It does not require a domain, account, Docker, or access to the internet after
the source and Go toolchain are available.

There are no official signed binary releases yet. Build and run the source
with Go 1.24 or newer. The project is pre-alpha; a successful smoke test
checks this small setup, not the capacity of a real application.

## 1. Get the source

Install [Git](https://git-scm.com/), [Go 1.24 or newer](https://go.dev/dl/),
and Python 3, then run:

```sh
git clone https://github.com/LucasLuchetta/Damocline.git
cd Damocline
go run ./cmd/damocline help
```

`go run` builds the CLI locally. On Windows, use PowerShell, Windows Terminal,
or CMD; the commands in this guide work from PowerShell. To install the
source-based launcher instead, follow [Source installation](SOURCE_INSTALL.md).

## 2. Start the synthetic server

Open a second terminal in the repository directory. Start Python's static
server bound to loopback so other devices cannot connect to it:

```powershell
py -3 -m http.server 18080 --bind 127.0.0.1
```

On Linux or macOS, use:

```sh
python3 -m http.server 18080 --bind 127.0.0.1
```

Leave this terminal open while running the test. The server only returns a
directory listing; it does not use real customer or application data.

## 3. Validate, then run the bounded test

In the first terminal, validate the included 2-user, 5-request-per-second,
10-second scenario. `--dry-run` sends no HTTP requests:

```sh
go run ./cmd/damocline run --config examples/local-smoke.json --dry-run
```

If validation succeeds and the local server is running, explicitly authorize
the traffic and save the report:

```sh
go run ./cmd/damocline run --config examples/local-smoke.json --authorized --out .damocline/reports/local-smoke.json
```

Inspect the saved report offline:

```sh
go run ./cmd/damocline report --file .damocline/reports/local-smoke.json
```

Stop the Python server with `Ctrl+C` when finished. The `.damocline/` directory
is local working data and is ignored by Git.

## 4. Read the result carefully

The report describes one bounded run against this local fixture. `PASS` means
the configured objectives passed in that run. It does not establish a maximum
number of users, long-term stability, or the capacity of another host or
application. For measurement definitions and uncertainty, see
[Understand your results](REPORT_EXPLAINER.md) and
[Measurement model](METRICS.md).

## Test an application you are authorized to assess

Only test systems you own or have explicit permission to test. Prefer a
staging environment and read-only routes; a `GET` request can still trigger
side effects. Begin with low traffic, coordinate with the operator, and treat
the standard limits as ceilings rather than recommended starting loads.
`--authorized` records your explicit confirmation; it does not verify
ownership or permission for you.

For the guided setup, supported authentication options, temporary browser
login, and local handling of session cookies, see
[Source installation](SOURCE_INSTALL.md) and [Guided CLI](INTERACTIVE_START.md).
For scenarios, advanced workloads, calibration, and host monitoring, see
[Advanced testing](ADVANCED_TESTING.md). Never commit credentials, cookies,
private scenarios, or reports containing sensitive target details.

## Current limits

- Damocline generates HTTP/HTTPS requests; it does not render pages in a
  browser or measure JavaScript, WebSockets, or browser-visible performance.
- Reusing one browser session across virtual users does not represent separate
  human identities. Use distinct authorized test accounts when that matters.
- Redirects are not followed by default. A redirect to a login page is
  reported as a failed request unless the scenario explicitly accounts for it.
- The standard safety ceilings are 50 virtual users, 100 requests per second,
  and three minutes. They are not safe recommendations for every target.
- Host monitoring is an optional separate command; collected metrics can
  provide context but do not prove the cause of a bottleneck.

See the [roadmap](ROADMAP.md) for current validation gaps and release status.
