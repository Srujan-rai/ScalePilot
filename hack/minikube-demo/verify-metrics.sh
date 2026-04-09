#!/usr/bin/env bash
# Run from repo root. Proves the demo app exposes metrics on the same URL Prometheus scrapes.
set -euo pipefail

echo "=== Pods in production ==="
kubectl get pods -n production -l app=web-frontend -o wide || true

echo ""
echo "=== Waiting for web-frontend Deployment (do not curl until Ready) ==="
if ! kubectl rollout status deployment/web-frontend -n production --timeout=180s; then
  echo ""
  echo "Rollout did not finish. Describe pod:"
  kubectl describe pod -n production -l app=web-frontend | tail -40
  exit 1
fi

echo ""
echo "=== From cluster: GET /metrics (must include http_requests_total) ==="
# Retry: Service / endpoints can lag Ready pods by a few seconds.
for attempt in 1 2 3 4 5 6; do
  VPOD="metrics-verify-${RANDOM}${RANDOM}"
  if kubectl run -n monitoring "${VPOD}" --rm --attach --restart=Never --image=curlimages/curl:8.5.0 -- \
    sh -ec 'curl -sS --max-time 15 "http://web-frontend.production.svc.cluster.local:8080/metrics" | tee /dev/stderr | grep -q http_requests_total'; then
    echo ""
    echo "OK. Wait ~30s for Prometheus scrapes, then query 127.0.0.1:9090 (with port-forward)."
    echo "Targets UI: http://localhost:9090/targets"
    exit 0
  fi
  echo "Attempt ${attempt}/6 failed, retrying in 5s..."
  sleep 5
done

echo ""
echo "FAILED: /metrics missing http_requests_total (wrong image, crash loop, or network policy)."
echo "Fix: build into Minikube, ensure pod Running:"
echo "  minikube image build -t web-frontend-metrics:demo -f hack/minikube-demo/Dockerfile ."
echo "  kubectl rollout restart deployment/web-frontend -n production"
exit 1
