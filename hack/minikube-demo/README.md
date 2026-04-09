# Minikube demo: Prometheus + synthetic metrics + ForecastPolicy

This walks through a **minimal** stack so ScalePilot can train an ARIMA model on real Prometheus samples and move the `ForecastPolicy` out of `ModelNotReady`.

**Stuck?** Run `./hack/minikube-demo/diagnose.sh` from the repo root and read the printed hints at the bottom.

## Prerequisites

- Minikube running (`minikube start`)
- Metrics Server (for the sample HPA): `minikube addons enable metrics-server`
- `production` namespace: `kubectl create namespace production --dry-run=client -o yaml | kubectl apply -f -`

## 1. Build the demo image where Minikube can run it

From the **repository root**. **Prefer this** — works with the **containerd** and **docker** runtimes (no `docker-env`, no Buildx “container” driver issues):

```bash
minikube image build -t web-frontend-metrics:demo -f hack/minikube-demo/Dockerfile .
```

Confirm the image is visible:

```bash
minikube image ls | grep web-frontend-metrics || true
```

**If `minikube image build` is unavailable or fails**, try loading from your host Docker (image must end up in Minikube’s store):

```bash
# Plain docker build (avoid Buildx-only drivers; forces classic builder + --load)
DOCKER_BUILDKIT=0 docker build --load -f hack/minikube-demo/Dockerfile -t web-frontend-metrics:demo .
minikube image load web-frontend-metrics:demo
```

**Docker-driver Minikube only** (legacy): `eval $(minikube docker-env)` then `docker build ...` — **not recommended** if Minikube uses **containerd** (you’ll see an “experimental” warning and `ImagePullBackOff` is common).

## 2. Deploy Prometheus + demo Deployment + Service + HPA

```bash
kubectl apply -f hack/minikube-demo/manifests.yaml
kubectl rollout status deployment/prometheus -n monitoring --timeout=120s
kubectl rollout status deployment/web-frontend -n production --timeout=120s
```

Check Prometheus sees the target: open in browser after port-forward (step 4) → **Status → Targets**, or:

```bash
kubectl port-forward -n monitoring svc/prometheus 9090:9090
# another shell:
curl -s 'http://127.0.0.1:9090/api/v1/targets' | head
```

## 3. Port-forward Prometheus (operator runs on the host)

With **`make run`**, the operator process uses **your laptop’s** network stack, so it cannot resolve `prometheus.monitoring` unless you point it at localhost:

```bash
kubectl port-forward -n monitoring svc/prometheus 9090:9090
```

Leave this running **the entire time** the operator is up. If this stops, `http://127.0.0.1:9090` stops working: training will fail (you should see **Reason: TrainingFailed** on the ForecastPolicy status with a Prometheus error in **Message**).

After restarting the operator, wait for the next retrain (or delete the ForecastPolicy and re-apply) so training runs again.

## 4. Apply the demo ForecastPolicy

```bash
kubectl apply -f hack/minikube-demo/forecastpolicy-demo.yaml
```

This sets `metricSource.address` to `http://127.0.0.1:9090`, shorter history, ARIMA(1,0,0) (needs fewer points than the default sample), and **`dryRun: true`** so you can confirm predictions without changing HPA `minReplicas`. Set `dryRun: false` when you want real patches.

## 5. Run the operator

In another terminal (with port-forward still up):

```bash
make run
```

## 6. If Prometheus queries are empty (`result:[]`)

That means **no samples were scraped** for `http_requests_total{deployment="web-frontend"}` yet. Common causes:

1. **Image not in Minikube** — `ErrImagePull` / `ImagePullBackOff` on `web-frontend`. Fix:
   ```bash
   minikube image build -t web-frontend-metrics:demo -f hack/minikube-demo/Dockerfile .
   kubectl delete pod -n production -l app=web-frontend
   ```
   Or `DOCKER_BUILDKIT=0 docker build --load ...` then `minikube image load web-frontend-metrics:demo`.
2. **Demo manifests not applied** — `kubectl apply -f hack/minikube-demo/manifests.yaml` and wait for pods Running.
3. **Prometheus started before the Service existed** — after `web-frontend` is healthy, restart Prometheus so scrapes succeed:
   ```bash
   kubectl rollout restart deployment/prometheus -n monitoring
   ```

From the repo root, run:

```bash
chmod +x hack/minikube-demo/verify-metrics.sh
./hack/minikube-demo/verify-metrics.sh
```

The script **waits for `deployment/web-frontend` to finish rolling out** (up to 3 minutes), then curls `/metrics` from inside the cluster with retries—so it is safe to run right after `kubectl delete pod` / `rollout restart`. You should see lines containing `http_requests_total`.

## 7. Wait and verify

ARIMA(1,0,0) needs **at least 11** points from the range query. If you see **TrainingFailed** with `insufficient data: need at least 11 points, got N`, Prometheus is working but the series is **too new** or **too sparse**—wait **~10 minutes** after targets are UP, or re-apply `forecastpolicy-demo.yaml` (it uses **15s** steps and **6h** history to collect samples faster).

```bash
kubectl describe forecastpolicy web-frontend-forecast -n production
kubectl get forecastpolicy -n production -o wide
```

You want the **Error** condition to clear (no **TrainingFailed** / **ModelNotReady**) and status fields like **CurrentPrediction** / **PredictedMinReplicas** to populate.

Operator logs should show model ConfigMap creation and eventually successful reconciles. If training fails, check logs for Prometheus errors (wrong address, port-forward down, empty query).

## In-cluster operator instead of `make run`

Set `metricSource.address` to `http://prometheus.monitoring:9090` and deploy the operator into the cluster (`make deploy` / Helm). Do not rely on `127.0.0.1:9090` from inside the pod.

## Optional load

The demo binary already increments the counter continuously. You can add HTTP load with:

```bash
kubectl run -it --rm curl --image=curlimages/curl --restart=Never -- \
  sh -c 'while true; do curl -s http://web-frontend.production.svc.cluster.local:8080/ >/dev/null; done'
```

Press Ctrl+C when done; the pod exits with `--rm`.
