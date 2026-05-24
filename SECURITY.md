# Security Policy

## Supported versions

reactive-policy is pre-1.0. Security fixes land on the latest minor release.

| Version | Supported          |
|---------|--------------------|
| 0.1.x   | :white_check_mark: |
| < 0.1   | :x:                |

## Reporting a vulnerability

Please report security issues privately. **Do not open a public GitHub issue
for a vulnerability.**

- Use GitHub's [private vulnerability reporting](https://github.com/Vedooo/reactive-policy/security/advisories/new)
  ("Report a vulnerability" under the Security tab), or
- email the maintainer at the address on the
  [GitHub profile](https://github.com/Vedooo).

Please include:

- affected version(s) or commit,
- a description and impact assessment,
- reproduction steps or a proof of concept,
- any suggested remediation.

You can expect an acknowledgement within 5 working days and a remediation plan
once the report is triaged.

## Scope and hardening notes

reactive-policy is an in-cluster operator that mutates cluster state. A few
things worth knowing when assessing risk:

- **RBAC is broad by design.** The `k8s.annotate` plugin's permissions cover
  arbitrary kinds because targets are policy-defined. Scope the ClusterRole down
  to the kinds you actually act on in production (see the chart `clusterrole.yaml`).
- **Actions can be destructive.** Pipelines run with the operator's service
  account. Treat `ReactivePolicy` create/update as a privileged operation and
  gate it with the validating webhook and your own RBAC.
- **Irreversible actions are opt-in.** The webhook rejects policies that use a
  non-reversible plugin unless `spec.allowIrreversible: true` is set.
- **Secrets** are read (e.g. the Slack webhook URL) but never written to logs or
  `ActionAudit` records.
- **Metrics** default to plain HTTP for ease of scraping; enable `metrics.secure`
  in the chart for authenticated/TLS metrics in hardened environments.

## Disclosure

We follow coordinated disclosure. Once a fix is available we will publish a
GitHub Security Advisory and credit the reporter unless anonymity is requested.
