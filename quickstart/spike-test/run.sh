#!/usr/bin/env bash
# ScalePilot Spike Test — end-to-end from cluster creation to ForecastPolicy demo
#
# Usage:
#   ./run.sh <gcp-project-id>
#
# What this does:
#   1. Creates a GKE Autopilot cluster (skips if already exists)
#   2. Installs Prometheus via Helm
#   3. Installs ScalePilot CRDs
#   4. Deploys spike-app + HPA + ForecastPolicy
#   5. Port-forwards Prometheus so the local operator can reach it
#   6. Starts the ScalePilot operator in the background
#   7. Runs the 4-act spike demo (baseline → train → spike → cooldown)
#
# Prerequisites: gcloud, kubectl, helm, go (for operator binary)
# Estimated cost: ~$0.40 for a 3-hour Autopilot test

set -euo pipefail

PROJECT_ID="${1:?Usage: ./run.sh <gcp-project-id>}"
CLUSTER_NAME="scalepilot-spike-test"
REGION="us-central1"
NS="production"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
step()  { echo -e "\n${BOLD}${CYAN}──────────────────────────────────────────${NC}"; echo -e "${BOLD}${CYAN}  $*${NC}"; echo -e "${BOLD}${CYAN}──────────────────────────────────────────${NC}"; }
die()   { echo -e "${RED}[ERR]${NC}   $*" >&2; exit 1; }

PIDS=()
cleanup_pids() {
  for pid in "${PIDS[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
}
trap cleanup_pids EXIT

# ── 0. Prerequisites ──────────────────────────────────────────────────────────
step "Step 0 — Prerequisites"
for cmd in gcloud kubectl helm go; do
  command -v "$cmd" &>/dev/null || die "$cmd is required but not installed."
  info "$cmd found: $(command -v "$cmd")"
done

gcloud config set project "$PROJECT_ID"

# ── 1. GKE Autopilot Cluster ──────────────────────────────────────────────────
step "Step 1 — GKE Autopilot Cluster"
if gcloud container clusters describe "$CLUSTER_NAME" --region "$REGION" &>/dev/null; then
  warn "Cluster $CLUSTER_NAME already exists — skipping creation."
else
  info "Creating Autopilot cluster $CLUSTER_NAME in $REGION (takes ~5 min)..."
  gcloud container clusters create-auto "$CLUSTER_NAME" \
    --region "$REGION" \
    --release-channel stable \
    --quiet
fi

info "Fetching credentials..."
gcloud container clusters get-credentials "$CLUSTER_NAME" --region "$REGION"

info "Waiting for cluster API to be reachable..."
until kubectl cluster-info &>/dev/null; do
  echo "  not ready yet, retrying in 10s..."
  sleep 10
done
info "Cluster is reachable."

# ── 2. Namespaces ─────────────────────────────────────────────────────────────
step "Step 2 — Namespaces"
kubectl create namespace "${NS}" --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace monitoring --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace scalepilot-system --dry-run=client -o yaml | kubectl apply -f -

# ── 3. Prometheus ─────────────────────────────────────────────────────────────
step "Step 3 — Prometheus"
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts --force-update &>/dev/null
helm repo update &>/dev/null

if helm status prometheus -n monitoring &>/dev/null; then
  warn "Prometheus already installed — skipping."
else
  info "Installing Prometheus (minimal — no alertmanager, no node-exporter)..."
  helm install prometheus prometheus-community/prometheus \
    --namespace monitoring \
    --set alertmanager.enabled=false \
    --set prometheus-pushgateway.enabled=false \
    --set prometheus-node-exporter.enabled=false \
    --set server.persistentVolume.size=2Gi \
    --wait --timeout 8m
fi

# ── 4. ScalePilot CRDs ────────────────────────────────────────────────────────
step "Step 4 — ScalePilot CRDs"
info "Installing CRDs from repo..."
make -C "$REPO_ROOT" manifests generate
kubectl apply -f "${REPO_ROOT}/config/crd/bases/" --validate=false

info "Waiting for CRDs to be registered in the API server..."
for crd in forecastpolicies.autoscaling.scalepilot.io federatedscaledobjects.autoscaling.scalepilot.io scalingbudgets.autoscaling.scalepilot.io; do
  until kubectl get crd "$crd" &>/dev/null; do
    echo "  $crd not ready yet, retrying in 5s..."
    sleep 5
  done
  info "CRD registered: $crd"
done

# ── 5. Deploy spike-app, HPA, ForecastPolicy ─────────────────────────────────
step "Step 5 — Deploy Spike Test Resources"
info "Applying spike-test manifests..."
kubectl apply -k "${SCRIPT_DIR}"

info "Waiting for spike-app deployment to be available..."
kubectl rollout status deployment/spike-app -n "${NS}" --timeout=3m

# ── 6. Port-forward Prometheus ────────────────────────────────────────────────
step "Step 6 — Port-forward Prometheus → localhost:9090"
info "Starting port-forward in background..."
kubectl port-forward svc/prometheus-server -n monitoring 9090:80 &>/dev/null &
PIDS+=($!)
sleep 5

# Quick sanity-check
if curl -sf http://localhost:9090/-/healthy &>/dev/null; then
  info "Prometheus is healthy at http://localhost:9090"
else
  warn "Prometheus health check failed — operator may have trouble querying metrics."
fi

# ── 7. Build & start operator ─────────────────────────────────────────────────
step "Step 7 — Build & Start ScalePilot Operator"
info "Building operator binary..."
make -C "$REPO_ROOT" build

info "Generating webhook TLS certs..."
make -C "$REPO_ROOT" webhook-certs

info "Starting operator in background (logs → /tmp/scalepilot-operator.log)..."
"${REPO_ROOT}/bin/manager" \
  --metrics-bind-address=:8081 \
  --health-probe-bind-address=:8082 \
  > /tmp/scalepilot-operator.log 2>&1 &
PIDS+=($!)
OPERATOR_PID=$!
sleep 5

if kill -0 "$OPERATOR_PID" 2>/dev/null; then
  info "Operator running (PID $OPERATOR_PID). Logs: tail -f /tmp/scalepilot-operator.log"
else
  die "Operator failed to start. Check: cat /tmp/scalepilot-operator.log"
fi

# ── 8. Wait for ForecastPolicy to collect initial data ────────────────────────
step "Step 8 — Collect Initial Metrics (60s)"
info "Giving ForecastPolicy 60s to collect initial CPU baseline before the demo..."
echo "  (This is the minimum — more history = better predictions)"
for i in $(seq 1 6); do
  sleep 10
  echo -e "  ${CYAN}[$(date +%H:%M:%S)]${NC} ForecastPolicy:"
  kubectl get forecastpolicy spike-app-forecast -n "${NS}" \
    --no-headers -o custom-columns="  PREDICTED:.status.predictedReplicas,ACTIVE:.status.active,LAST_RETRAIN:.status.lastRetrainTime" 2>/dev/null || true
done

# ── 9. Run the spike demo ─────────────────────────────────────────────────────
step "Step 9 — Running Spike Demo"
echo ""
echo -e "${BOLD}What you will see:${NC}"
echo "  ACT 1 (0-30s)   — Baseline: spike-app at 1 replica, no load"
echo "  ACT 2 (30-120s) — Train: light load builds CPU history for ARIMA"
echo "  ACT 3 (120-240s)— Spike: 20 load pods + high CPU → ScalePilot pre-warms HPA minReplicas"
echo "  ACT 4 (240-330s)— Cool down: load removed, replicas return to 1"
echo ""
echo -e "Operator logs: ${CYAN}tail -f /tmp/scalepilot-operator.log${NC}"
echo ""

bash "${SCRIPT_DIR}/spike-test.sh"

# ── Done ──────────────────────────────────────────────────────────────────────
step "All done"
echo ""
echo -e "${BOLD}To delete the cluster when finished:${NC}"
echo "  gcloud container clusters delete ${CLUSTER_NAME} --region ${REGION} --quiet"
echo ""
echo -e "${BOLD}To re-run just the demo (cluster stays up):${NC}"
echo "  bash ${SCRIPT_DIR}/spike-test.sh"
echo ""
