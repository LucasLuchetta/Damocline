# Project governance

Damocline is open source, but its upstream repository is **maintainer-governed**.
Public source code and the ability to fork do not give anyone permission to
push to the default branch, demand a merge, or define the project's direction.

## Maintainers and decision authority

- Current lead maintainer: [@LucasLuchetta](https://github.com/LucasLuchetta).
- Maintainers decide the roadmap, architecture, release contents, security
  response, review standards, and whether a change is accepted.
- Only explicitly authorized maintainers may merge, publish releases, or
  change repository policy. Contributors do not become maintainers by
  submitting a PR or having one accepted.
- Maintainers may request revisions, defer a proposal, or close it without
  merging. A passing CI run, an approved issue, or a CODEOWNERS notification
  does not guarantee acceptance.
- The Apache-2.0 license permits lawful reuse and forks; upstream inclusion
  and use of the project's name or branding are separate questions.

## Contribution paths

| Type of contribution | Entry point | Requirement before substantial coding |
| --- | --- | --- |
| Documentation typo or narrowly scoped fix | Focused PR | No advance approval required |
| Reproducible defect or regression | Bug report | Discuss when the fix changes observable behavior or metrics |
| New feature or behavior change | Feature proposal | Maintainer agreement on scope and validation |
| Architecture, load model, measurement, safety, data schema, dependencies, release policy | Feature proposal with design rationale | Explicit maintainer go-ahead; ADR when applicable |
| Security vulnerability | Private vulnerability report | Never open a public issue with exploit details or secrets |

The roadmap is maintained by the project. Open issues and suggestions are
not automatically approved work items. Please avoid speculative refactors,
broad rewrite PRs, drive-by dependency upgrades, generated bulk changes,
and unsolicited large feature implementations.

## Pull request acceptance

All changes to the upstream default branch should go through a pull request.
Merging is a maintainer decision, not a contributor entitlement. A review
should establish, where relevant:

1. **Scope:** A focused diff addresses an accepted problem and preserves the
   project's stated scientific and product boundaries.
2. **Correctness:** New behavior has reproducible tests, including failure,
   cancellation, boundary, and compatibility cases when applicable.
3. **Measurement credibility:** Metrics distinguish observed facts from
   hypotheses. Capacity claims disclose workload, conditions, objectives,
   duration, uncertainty and generator limits.
4. **Safety:** No unauthorized targets, unbounded traffic, real credentials,
   private customer data, or unsafe defaults.
5. **Evidence:** The PR lists verification performed and remaining gaps.
   Claims of performance or equivalence require suitable controlled evidence.
6. **Maintainability:** Documentation, examples, report schemas, ADRs and
   CHANGELOG are updated where behavior or contracts change.
7. **Human accountability:** For AI-assisted work, the submitter must review
   the complete diff, be able to explain it and take responsibility for
   testing, provenance and licensing.

Code owners identify reviewers but do not replace branch protection.
CI results are evidence, not an authorization to merge.

## Review and release policy

Maintainers determine review order and may prioritize correctness,
security and reproducibility over feature volume. There is no promised
turnaround time and no commitment to accept unsolicited contributions.
Pre-alpha does not imply a stability or backward-compatibility guarantee;
intentional breaking changes still require explicit documentation.

## Participation

Keep discussions technical and respectful. Harassment, threats, spam,
disclosure of private information, and attempts to bypass review or safety
controls are not accepted. Maintainers may moderate, close or lock
discussions and may restrict participation consistent with GitHub policy.

For practical setup and PR instructions, see [CONTRIBUTING.md](CONTRIBUTING.md).
For private security disclosure, see [SECURITY.md](SECURITY.md).
