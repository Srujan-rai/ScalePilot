#!/usr/bin/env bash
# One-shot health check for the Minikube metrics demo (run from repo root).
set -uo pipefail

echo "========== 0) Is Minikube actually running? =========="
if minikube status 2>/dev/null | grep -q 'apiserver: Running'; then
	echo "OK — apiserver is Running."
else
	echo ""
	echo ">>> PRIMARY ISSUE: Minikube is NOT running (or unreachable). <<<"
	echo "    Your log showed: host: Stopped, apiserver: Stopped."
	echo "    kubectl context may still say 'minikube', but there is no cluster."
	echo ""
	echo "    Fix:"
	echo "      minikube start"
	echo "    Then install CRDs if needed:"
	echo "      make install"
	echo "    Then follow hack/minikube-demo/README.md from step 1."
	echo ""
	MINIKUBE_DOWN=1
fi

echo ""
echo "========== 1) kubectl context =========="
kubectl config current-context 2>/dev/null || echo "(no context)"

echo ""
echo "========== 2) Minikube status (full) =========="
minikube status 2>/dev/null || echo "minikube CLI failed (not installed?)"

echo ""
echo "========== 3) Image on Minikube node =========="
if [ -n "${MINIKUBE_DOWN:-}" ]; then
	echo "(skipped — start Minikube first)"
else
	minikube image ls 2>/dev/null | grep -E 'web-frontend|REPOSITORY' || echo "(no web-frontend-metrics image — run: minikube image build -t web-frontend-metrics:demo -f hack/minikube-demo/Dockerfile .)"
fi

echo ""
echo "========== 4) production / web-frontend =========="
if [ -n "${MINIKUBE_DOWN:-}" ]; then
	echo "(skipped — start Minikube first)"
else
	kubectl get deploy,pods,svc,endpoints -n production -l app=web-frontend 2>/dev/null || kubectl get deploy,pods,svc,endpoints -n production 2>/dev/null || echo "namespace production missing or API unreachable — apply manifests after minikube start"
fi

echo ""
echo "========== 5) Pod events (production) =========="
if [ -z "${MINIKUBE_DOWN:-}" ]; then
	kubectl get events -n production --field-selector involvedObject.kind=Pod --sort-by='.lastTimestamp' 2>/dev/null | tail -8 || true
fi

echo ""
echo "========== 6) monitoring / Prometheus =========="
if [ -n "${MINIKUBE_DOWN:-}" ]; then
	echo "(skipped — start Minikube first)"
else
	kubectl get deploy,pods -n monitoring -l app=prometheus 2>/dev/null || echo "(nothing in monitoring — kubectl apply -f hack/minikube-demo/manifests.yaml)"
fi

echo ""
echo "========== 7) ForecastPolicy / CRDs =========="
if [ -n "${MINIKUBE_DOWN:-}" ]; then
	echo "(skipped — start Minikube first)"
else
	kubectl get forecastpolicy -n production -o wide 2>/dev/null || echo "(no ForecastPolicy or CRDs not installed — run: make install)"
fi

echo ""
echo "========== Interpretation =========="
if [ -n "${MINIKUBE_DOWN:-}" ]; then
	echo "- **Nothing below matters until:** minikube start"
fi
echo "- ImagePullBackOff → minikube image build … (see README)"
echo "- Empty Prometheus → web-frontend Running, then rollout restart prometheus, port-forward :9090"
echo "- make run + 127.0.0.1:9090 → keep port-forward open"
