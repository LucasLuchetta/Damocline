# Security policy

## Supported versions

Damocline is **pre-alpha**. There is no stable release, supported-version
matrix, security fix SLA, or guarantee of vulnerability response times.

## Private vulnerability disclosure

Please **do not open public issues or pull requests** containing exploitable
vulnerabilities, secret values, private application details or credentials.

Use GitHub private vulnerability reporting at **Security > Advisories >
Report a vulnerability** if the repository makes that feature available.
Otherwise, contact the maintainer through their public GitHub profile to
arrange a private channel **without including exploit details in a public
message**. Do not post sensitive proof-of-concept traffic or artifacts.

Include an affected commit/version, impact, safe reproduction steps and
suggested remediation where possible. Do not test on third-party targets
without their authorization.

## Existing implementation and safety boundaries

Damocline already has an experimental native HTTP load generator. Its
pre-alpha status does **not** mean it is a non-networking scaffold, nor
does it imply the generator is suitable for arbitrary public targets.
Current safety boundaries include bounded load configurations, explicit
authorization controls, generator-health checks, and redaction-oriented
report handling. Their presence is not proof of complete security.

In particular:

- Only test systems you own or are explicitly authorized to assess.
- Keep concurrency, rate, duration, timeouts and abort conditions bounded.
- Never commit credentials, session cookies, sensitive HAR files, private
  URLs or production reports to the public repository.
- Prefer read-only synthetic fixtures for validation. Mutating scenarios
  require deliberate authorization and a cleanup plan.
- Treat metrics, calibration and claimed causes as bounded evidence, not
  guarantees of application capacity or vulnerability absence.

See [CONTRIBUTING.md](CONTRIBUTING.md) and
[GOVERNANCE.md](GOVERNANCE.md) for review and responsible-submission rules.
