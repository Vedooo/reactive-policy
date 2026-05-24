---
name: Bug report
about: Report something that isn't working as documented
title: "[bug] "
labels: bug
---

**What happened**

A clear description of the bug.

**What you expected**

What you expected to happen instead.

**Reproduction**

Steps, plus the relevant `ReactivePolicy` (redact secrets) and the output of:

```bash
rp policy describe <name> -n <ns> -o yaml
rp action audit -n <ns>
```

**Environment**

- reactive-policy version (chart / image tag):
- Kubernetes version:
- Install method (Helm OCI / manifests):
- Metric source (Prometheus version):

**Operator logs**

```text
kubectl logs -n reactive-policy deploy/reactive-policy
```
