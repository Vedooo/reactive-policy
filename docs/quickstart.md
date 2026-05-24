# Quickstart

Go from nothing to a policy that **visibly annotates a Deployment** when a metric
crosses its threshold — on a local `kind` cluster, in about five minutes. No real
Prometheus needed: a tiny mock metric source makes the trigger deterministic.

## Prerequisites

- [`kind`](https://kind.sigs.k8s.io/), `kubectl`, and `helm` 3.8+
- The [`rp` CLI](installation.md#install-the-rp-cli) on your `PATH`

## 1. Create a cluster and install the operator

```bash
kind create cluster --name rp-demo

helm install reactive-policy \
  oci://ghcr.io/vedooo/charts/reactive-policy \
  --namespace reactive-policy --create-namespace \
  --set metrics.enabled=true --set metrics.secure=false

kubectl wait --for=condition=Available deploy -n reactive-policy --all --timeout=120s
```

## 2. Deploy a workload and a mock metric source

The mock always reports an error rate of `0.42`, so the policy reliably crosses
its `0.05` threshold.

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: v1
kind: Namespace
metadata: { name: demo }
---
apiVersion: apps/v1
kind: Deployment
metadata: { name: api-service, namespace: demo, labels: { app: api-service } }
spec:
  replicas: 1
  selector: { matchLabels: { app: api-service } }
  template:
    metadata: { labels: { app: api-service } }
    spec:
      containers:
        - { name: api, image: nginx:1.27-alpine, ports: [{ containerPort: 80 }] }
---
apiVersion: v1
kind: ConfigMap
metadata: { name: mock-prometheus, namespace: demo }
data:
  default.conf: |
    server {
      listen 9090;
      location = /api/v1/query {
        default_type application/json;
        return 200 '{"status":"success","data":{"resultType":"scalar","result":[1700000000,"0.42"]}}';
      }
    }
---
apiVersion: apps/v1
kind: Deployment
metadata: { name: mock-prometheus, namespace: demo, labels: { app: mock-prometheus } }
spec:
  replicas: 1
  selector: { matchLabels: { app: mock-prometheus } }
  template:
    metadata: { labels: { app: mock-prometheus } }
    spec:
      containers:
        - name: nginx
          image: nginx:1.27-alpine
          ports: [{ containerPort: 9090 }]
          volumeMounts: [{ name: conf, mountPath: /etc/nginx/conf.d }]
      volumes:
        - { name: conf, configMap: { name: mock-prometheus } }
---
apiVersion: v1
kind: Service
metadata: { name: mock-prometheus, namespace: demo }
spec:
  selector: { app: mock-prometheus }
  ports: [{ port: 9090, targetPort: 9090 }]
EOF
```

## 3. Apply a policy

It watches the mock metric and, when the error rate stays above 5% for 30s,
annotates the `api-service` Deployment.

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: reactive-policy.io/v1alpha1
kind: ReactivePolicy
metadata: { name: error-rate-guard, namespace: demo }
spec:
  target:
    selector: { matchLabels: { app: api-service } }
    kinds: [{ apiVersion: apps/v1, kind: Deployment }]
    maxResources: 5
  observe:
    source: prometheus
    endpoint: http://mock-prometheus.demo:9090
    query: 'sum(rate(http_requests_total{status=~"5.."}[2m])) / sum(rate(http_requests_total[2m]))'
    threshold: "0.05"
    operator: GreaterThan
    duration: 30s
    pollInterval: 10s
  actions:
    - plugin: k8s.annotate
      params:
        key: "reactive-policy.io/incident"
        value: "error rate {{ .MetricValue }} crossed threshold at {{ .Timestamp }}"
  cooldown: 1m
  maxTriggersPerHour: 5
EOF
```

## 4. Watch it trigger

```bash
kubectl get reactivepolicy error-rate-guard -n demo -w
```

Within ~30–40s the state moves `Watching → Triggering → Cooldown` and `COUNT`
becomes `1`. The Deployment is now annotated:

```bash
kubectl get deploy api-service -n demo \
  -o jsonpath='{.metadata.annotations.reactive-policy\.io/incident}'
# → error rate 0.42 crossed threshold at 2026-...
```

## 5. Inspect with `rp`

```console
$ rp policy list -A
NAMESPACE   NAME               STATE      LAST TRIGGERED   COUNT   VALUE   AGE
demo        error-rate-guard   Cooldown   15s              1       0.42    45s

$ rp action history error-rate-guard -n demo
TRIGGERED   AUDIT                    ACTION           STATUS      REVERSIBLE   MESSAGE
23s ago     error-rate-guard-2z7jk   0.k8s.annotate   Succeeded   true         annotated Deployment api-service ...
```

## 6. Revert

```bash
rp action audit -n demo                          # copy an AUDIT name
rp action revert <audit-name> -n demo
```

A few seconds later the annotation is gone and the audit shows `REVERTED true`.

## 7. Try the fan-out

Add a second matching Deployment and watch the next trigger act on both:

```bash
kubectl create deployment api-service-2 --image=nginx:1.27-alpine -n demo
kubectl label deployment api-service-2 app=api-service -n demo
# after the next trigger, `rp action history` shows two records
```

## Clean up

```bash
kind delete cluster --name rp-demo
```

Next: read the [Concepts](concepts.md) or the full [CLI reference](cli.md).
