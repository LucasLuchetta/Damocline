# Security policy

## Supported versions

Damocline is **pre-alpha** and has no released or security-supported version.

## Reporting vulnerabilities

Please **do not file public issues** containing exploitable vulnerabilities, sensitive data, or credentials.

Use GitHub private vulnerability reporting (**Security > Advisories > Report a vulnerability**) if enabled for this repository. If it is not available, arrange a private communication channel with the maintainer through their public GitHub profile without posting sensitive details.

There is no formal response-time SLA at this stage.

## Planned security requirements

Before any load generation is shipped, the engine must:

- Restrict testing to targets users own or are authorized to assess.
- Require explicit concurrency/rate/duration bounds, timeouts, and stop conditions.
- Avoid arbitrary host discovery, destructive defaults, and production write operations without opt-in.
- Redact credentials, session cookies, tokens, private URLs, and sensitive response data from artifacts.
- Monitor its own resource constraints and stop safely when necessary.

The existing CLI scaffold does not send load-test traffic.
