#!/usr/bin/env bash
# ScalePilot ForecastPolicy Demo
# Starts everything, drives load waves, shows live terminal dashboard.
# Usage: ./demo.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
NS="production"
PIDS=()

# ── colours ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; DIM='\033[2m'; NC='\033[0m'
CLEAR='\033[2J\033[H'

info() { echo -e "${GREEN}[setup]${NC} $*"; }
warn() { echo -e "${YELLOW}[warn]${NC}  $*"; }

# ── cleanup on exit ───────────────────────────────────────────────────────────
cleanup() {
  echo -e "\n${YELLOW}Cleaning up...${NC}"
  kubectl scale deployment/spike-load-generator -n "$NS" --replicas=0 2>/dev/null || true
  for pid in "${PIDS[@]}"; do kill "$pid" 2>/dev/null || true; done
  echo "Done."
}
trap cleanup EXIT INT TERM

# ── pre-flight ────────────────────────────────────────────────────────────────
for cmd in kubectl make; do
  command -v "$cmd" &>/dev/null || { echo "$cmd not found"; exit 1; }
done
kubectl get deployment spike-app -n "$NS" &>/dev/null || {
  echo "spike-app not deployed. Run: kubectl apply -k quickstart/spike-test/"
  exit 1
}

# ── start operator ────────────────────────────────────────────────────────────
echo -e "${BOLD}Starting ScalePilot operator...${NC}"
make -C "$REPO_ROOT" webhook-certs -s
make -C "$REPO_ROOT" build -s

# Kill any previously running manager
pkill -f "bin/manager" 2>/dev/null || true
sleep 1

"${REPO_ROOT}/bin/manager" \
  --metrics-bind-address=:8081 \
  --health-probe-bind-address=:8082 \
  > /tmp/scalepilot.log 2>&1 &
PIDS+=($!)
OPERATOR_PID=$!
sleep 3

kill -0 "$OPERATOR_PID" 2>/dev/null || {
  echo "Operator failed to start:"
  tail -10 /tmp/scalepilot.log
  exit 1
}
info "Operator running (PID $OPERATOR_PID)"

# ── port-forward prometheus ───────────────────────────────────────────────────
pkill -f "port-forward.*prometheus-server" 2>/dev/null || true
sleep 1
kubectl port-forward svc/prometheus-server -n monitoring 9090:80 &>/dev/null &
PIDS+=($!)
sleep 3

curl -sf http://localhost:9090/-/healthy &>/dev/null \
  && info "Prometheus reachable at localhost:9090" \
  || warn "Prometheus health check failed — metrics may be delayed"

# ── grafana ───────────────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
GRAFANA_PORT=3000

if ! helm status grafana -n monitoring &>/dev/null; then
  info "Installing Grafana..."
  helm repo add grafana https://grafana.github.io/helm-charts --force-update &>/dev/null
  helm repo update &>/dev/null
  helm install grafana grafana/grafana \
    --namespace monitoring \
    --set adminPassword=scalepilot \
    --set persistence.enabled=false \
    --set datasources."datasources\.yaml".apiVersion=1 \
    --set datasources."datasources\.yaml".datasources[0].name=Prometheus \
    --set datasources."datasources\.yaml".datasources[0].type=prometheus \
    --set datasources."datasources\.yaml".datasources[0].uid=prometheus \
    --set datasources."datasources\.yaml".datasources[0].url=http://prometheus-server.monitoring.svc.cluster.local \
    --set datasources."datasources\.yaml".datasources[0].isDefault=true \
    --wait --timeout 5m
fi

pkill -f "port-forward.*grafana" 2>/dev/null || true
sleep 1
kubectl port-forward svc/grafana -n monitoring ${GRAFANA_PORT}:80 &>/dev/null &
PIDS+=($!)
sleep 5

DASHBOARD_JSON="$(cat "${SCRIPT_DIR}/spike-dashboard.json")"
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "http://admin:scalepilot@localhost:${GRAFANA_PORT}/api/dashboards/db" \
  -H "Content-Type: application/json" \
  -d "{\"dashboard\": ${DASHBOARD_JSON}, \"overwrite\": true, \"folderId\": 0}")

if [[ "$HTTP_STATUS" == "200" ]]; then
  info "Grafana dashboard loaded at http://localhost:${GRAFANA_PORT} (admin / scalepilot)"
else
  warn "Dashboard upload returned HTTP ${HTTP_STATUS} — open Grafana and import spike-dashboard.json manually"
fi

# ── apply ForecastPolicy if not present ──────────────────────────────────────
kubectl apply -f "${SCRIPT_DIR}/spike-forecastpolicy.yaml" &>/dev/null
info "ForecastPolicy applied"

echo ""
echo -e "${BOLD}${GREEN}Open Grafana now:${NC} http://localhost:${GRAFANA_PORT}  (admin / scalepilot)"
echo -e "${BOLD}Dashboard:${NC} ScalePilot — Predictive Scaling Demo"
echo ""
echo -e "${BOLD}Starting demo in 5 seconds...${NC}"
sleep 5

PHASE_FILE="/tmp/scalepilot-phase"
echo "Initialising|$DIM" > "$PHASE_FILE"

# ── dashboard function ────────────────────────────────────────────────────────
dashboard() {
  local fp_predicted fp_prediction fp_active fp_retrain
  local hpa_min hpa_desired hpa_current hpa_max hpa_target hpa_actual
  local load_ready phase_line PHASE_LABEL PHASE_COLOR

  phase_line=$(cat "$PHASE_FILE" 2>/dev/null || echo "Initialising|$DIM")
  PHASE_LABEL="${phase_line%%|*}"
  PHASE_COLOR="${phase_line##*|}"

  fp_predicted=$(kubectl get forecastpolicy spike-app-forecast -n "$NS" \
    -o jsonpath='{.status.predictedMinReplicas}' 2>/dev/null || echo "-")
  fp_prediction=$(kubectl get forecastpolicy spike-app-forecast -n "$NS" \
    -o jsonpath='{.status.currentPrediction}' 2>/dev/null || echo "-")
  fp_active=$(kubectl get forecastpolicy spike-app-forecast -n "$NS" \
    -o jsonpath='{.status.activeMinReplicas}' 2>/dev/null || echo "-")
  fp_retrain=$(kubectl get forecastpolicy spike-app-forecast -n "$NS" \
    -o jsonpath='{.status.lastTrainedAt}' 2>/dev/null | sed 's/T/ /;s/Z//' || echo "not yet")

  hpa_min=$(kubectl get hpa spike-app-hpa -n "$NS" \
    -o jsonpath='{.spec.minReplicas}' 2>/dev/null || echo "-")
  hpa_desired=$(kubectl get hpa spike-app-hpa -n "$NS" \
    -o jsonpath='{.status.desiredReplicas}' 2>/dev/null || echo "-")
  hpa_current=$(kubectl get hpa spike-app-hpa -n "$NS" \
    -o jsonpath='{.status.currentReplicas}' 2>/dev/null || echo "-")
  hpa_max=$(kubectl get hpa spike-app-hpa -n "$NS" \
    -o jsonpath='{.spec.maxReplicas}' 2>/dev/null || echo "-")
  hpa_target=$(kubectl get hpa spike-app-hpa -n "$NS" \
    -o jsonpath='{.spec.metrics[0].resource.target.averageUtilization}' 2>/dev/null || echo "-")
  hpa_actual=$(kubectl get hpa spike-app-hpa -n "$NS" \
    -o jsonpath='{.status.currentMetrics[0].resource.current.averageUtilization}' 2>/dev/null || echo "0")

  load_ready=$(kubectl get deployment spike-load-generator -n "$NS" \
    -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
  [[ -z "$load_ready" ]] && load_ready="0"

  # CPU bar
  local bar="" pct="${hpa_actual:-0}"
  local filled=$(( pct * 20 / 100 )) 2>/dev/null || filled=0
  for ((i=0; i<filled && i<20; i++)); do bar+="█"; done
  for ((i=filled; i<20; i++)); do bar+="░"; done
  local bar_color="$GREEN"
  [[ "$pct" -gt 50 ]] 2>/dev/null && bar_color="$YELLOW"
  [[ "$pct" -gt 80 ]] 2>/dev/null && bar_color="$RED"

  printf "${CLEAR}"
  echo -e "${BOLD}╔══════════════════════════════════════════════════════╗${NC}"
  echo -e "${BOLD}║        ScalePilot ForecastPolicy Live Demo           ║${NC}"
  echo -e "${BOLD}╚══════════════════════════════════════════════════════╝${NC}"
  echo ""
  echo -e "  ${BOLD}Phase:${NC} ${PHASE_COLOR}${PHASE_LABEL}${NC}   ${DIM}$(date +%H:%M:%S)${NC}"
  echo ""
  echo -e "  ${BOLD}┌─ ForecastPolicy (ARIMA prediction) ────────────────┐${NC}"
  echo -e "  │  Predicted Min Replicas : ${CYAN}${BOLD}${fp_predicted}${NC}"
  echo -e "  │  Current Prediction     : ${fp_prediction} CPU cores"
  echo -e "  │  Active Min Replicas    : ${fp_active}"
  echo -e "  │  Last Retrain           : ${DIM}${fp_retrain}${NC}"
  echo -e "  ${BOLD}└────────────────────────────────────────────────────┘${NC}"
  echo ""
  echo -e "  ${BOLD}┌─ HPA (spike-app-hpa) ───────────────────────────────┐${NC}"
  echo -e "  │  minReplicas (set by ScalePilot) : ${YELLOW}${BOLD}${hpa_min}${NC}"
  echo -e "  │  desiredReplicas                 : ${hpa_desired}"
  echo -e "  │  currentReplicas                 : ${GREEN}${BOLD}${hpa_current}${NC}"
  echo -e "  │  maxReplicas                     : ${hpa_max}"
  echo -e "  │  CPU Target / Actual             : ${hpa_target}% / ${hpa_actual}%"
  echo -e "  │  CPU  [${bar_color}${bar}${NC}] ${hpa_actual}%"
  echo -e "  ${BOLD}└────────────────────────────────────────────────────┘${NC}"
  echo ""
  echo -e "  ${BOLD}┌─ Load Generator ────────────────────────────────────┐${NC}"
  echo -e "  │  Pods sending traffic : ${RED}${BOLD}${load_ready}${NC}"
  echo -e "  ${BOLD}└────────────────────────────────────────────────────┘${NC}"
  echo ""
  echo -e "  ${DIM}Operator logs: tail -f /tmp/scalepilot.log${NC}"
  echo -e "  ${DIM}Press Ctrl+C to stop${NC}"
}

# ── load phase driver (background) ───────────────────────────────────────────
set_phase() { echo "$1|$2" > "$PHASE_FILE"; }

drive_load() {
  # Phase 1 — Baseline (5 min): model trains on idle, learns low CPU pattern
  set_phase "Phase 1/4 — Baseline  [5 min]  model trains on idle CPU" "$GREEN"
  kubectl scale deployment/spike-load-generator -n "$NS" --replicas=0 2>/dev/null
  sleep 300

  # Phase 2 — Ramp up (8 min): gradual load, model retrains once and sees rising trend
  set_phase "Phase 2/4 — Ramp up   [8 min]  5 load pods, model sees rising trend" "$YELLOW"
  kubectl scale deployment/spike-load-generator -n "$NS" --replicas=5 2>/dev/null
  kubectl set env deployment/spike-load-generator -n "$NS" LOAD_PARAM=30000 2>/dev/null
  sleep 480

  # Phase 3 — Full spike (12 min): moderate spike so ScalePilot pre-warming is visible
  set_phase "Phase 3/4 — SPIKE     [12 min] 10 load pods, ScalePilot pre-warms HPA" "$RED"
  kubectl scale deployment/spike-load-generator -n "$NS" --replicas=10 2>/dev/null
  kubectl set env deployment/spike-load-generator -n "$NS" LOAD_PARAM=50000 2>/dev/null
  sleep 720

  # Phase 4 — Cooldown (5 min): load removed, watch replicas drain back to 1
  set_phase "Phase 4/4 — Cooldown  [5 min]  load removed, replicas draining to 1" "$CYAN"
  kubectl scale deployment/spike-load-generator -n "$NS" --replicas=0 2>/dev/null
  kubectl set env deployment/spike-load-generator -n "$NS" LOAD_PARAM=50000 2>/dev/null
  sleep 300

  set_phase "Done — model has seen full spike cycle, predictions now reliable" "$GREEN"
}

# ── run ───────────────────────────────────────────────────────────────────────
drive_load &
PIDS+=($!)

while kill -0 "${PIDS[-1]}" 2>/dev/null; do
  dashboard
  sleep 5
done

# Final snapshot
dashboard
echo ""
echo -e "${GREEN}${BOLD}Demo complete.${NC}"
echo -e "What you observed:"
echo -e "  Phase 1: HPA minReplicas = 1, no CPU pressure"
echo -e "  Phase 2: CPU rises → ForecastPolicy retrains → PREDICTED increases"
echo -e "  Phase 3: ScalePilot raises HPA minReplicas BEFORE desiredReplicas spikes"
echo -e "  Phase 4: Load gone → replicas drain back to 1"
