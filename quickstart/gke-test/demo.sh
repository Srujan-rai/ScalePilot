#!/usr/bin/env bash
# ScalePilot Live Demo Script
# Walks through all 3 features with live traffic and visual feedback.
# Usage: ./demo.sh

set -euo pipefail

GREEN='\033[0;32m'; BLUE='\033[0;34m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'
BOLD='\033[1m'; NC='\033[0m'

header() {
  echo ""
  echo -e "${BOLD}${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${BOLD}${BLUE}  $*${NC}"
  echo -e "${BOLD}${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo ""
}

step() { echo -e "${CYAN}▶ $*${NC}"; }
info() { echo -e "${GREEN}  $*${NC}"; }
pause() {
  echo ""
  echo -e "${YELLOW}  Press ENTER to continue...${NC}"
  read -r
}

# ── Verify prerequisites ──────────────────────────────────────────────────────
kubectl cluster-info &>/dev/null || { echo "kubectl not connected to a cluster"; exit 1; }

# ── Check Grafana is running ──────────────────────────────────────────────────
GRAFANA_URL="http://localhost:3000"
if ! curl -s "$GRAFANA_URL" &>/dev/null; then
  echo -e "${YELLOW}Grafana not reachable at $GRAFANA_URL${NC}"
  echo "Run this in a separate terminal first:"
  echo "  kubectl port-forward svc/grafana -n monitoring 3000:80"
  echo ""
  echo "Then open: $GRAFANA_URL (admin / prom-operator)"
  echo ""
  echo -e "${YELLOW}  Press ENTER to continue anyway...${NC}"
  read -r
fi

clear
header "🚀 ScalePilot — Live Demo"
echo "  This demo shows all 3 ScalePilot features in action:"
echo "  1. Predictive Scaling     — ARIMA forecasts ahead of traffic spikes"
echo "  2. Multi-Cluster Federation — overflow to secondary clusters"
echo "  3. FinOps Budget Guard    — cost-aware scaling decisions"
echo ""
echo "  Grafana dashboard: $GRAFANA_URL (admin / prom-operator)"
echo "  Dashboard: ScalePilot Demo"
pause

# ═══════════════════════════════════════════════════════════════════════════════
header "📊 Baseline — Current State"
# ═══════════════════════════════════════════════════════════════════════════════

step "Current deployments in production namespace:"
kubectl get deployments -n production
echo ""

step "HPA status:"
kubectl get hpa -n production
echo ""

step "ForecastPolicy status:"
kubectl get forecastpolicy -n production
echo ""

step "FederatedScaledObject status:"
kubectl get federatedscaledobject -n production
echo ""
pause

# ═══════════════════════════════════════════════════════════════════════════════
header "🔥 DEMO ACT 1 — Predictive Scaling (Feature 1)"
# ═══════════════════════════════════════════════════════════════════════════════

info "Traditional HPA reacts AFTER your app is already under stress."
info "ScalePilot's ForecastPolicy reads traffic history, trains an ARIMA model,"
info "and raises HPA minReplicas BEFORE the spike arrives."
echo ""

step "Enabling predictive scaling (turning off dryRun)..."
kubectl patch forecastpolicy web-frontend-forecast -n production \
  --type=merge --patch '{"spec":{"dryRun":false}}'
echo ""

step "Starting traffic spike (10 load generator pods)..."
kubectl scale deployment load-generator -n production --replicas=10
echo ""

info "👀 Watch the Grafana dashboard — 'Predictive Scaling' panel"
info "   You'll see HPA Min Replicas rise as the forecast kicks in."
echo ""

step "Live HPA status (watching for 60s)..."
timeout 60 kubectl get hpa web-frontend-hpa -n production -w || true
echo ""

step "Current ForecastPolicy — predicted vs active:"
kubectl get forecastpolicy web-frontend-forecast -n production
echo ""
pause

step "Scaling down load..."
kubectl scale deployment load-generator -n production --replicas=0
info "HPA will scale down after stabilization window (~5 min)."
echo ""
pause

# ═══════════════════════════════════════════════════════════════════════════════
header "🌍 DEMO ACT 2 — Multi-Cluster Federation (Feature 2)"
# ═══════════════════════════════════════════════════════════════════════════════

info "When your primary cluster is saturated, ScalePilot automatically"
info "spills overflow workloads to secondary clusters — by priority order."
echo ""

step "Current FederatedScaledObject state:"
kubectl get federatedscaledobject order-processor-federation -n production -o wide
echo ""

step "Simulating primary cluster saturation (metric returns 5, primary has 1)..."
info "This triggers: desiredOverflow = ceil(5) - 1 = 4 overflow replicas"
echo ""

kubectl patch federatedscaledobject order-processor-federation -n production \
  --type=merge --patch '{"spec":{"metric":{"query":"vector(5)"}}}'
echo ""

step "Watching federation decisions (30s)..."
timeout 30 kubectl get federatedscaledobject order-processor-federation -n production -w || true
echo ""

step "Checking overflow deployment created in cluster:"
kubectl get deployments -n production
echo ""

info "👀 Grafana: 'Multi-Cluster Federation' panel shows primary vs overflow replicas"
pause

step "Removing overflow (metric back to 0)..."
kubectl patch federatedscaledobject order-processor-federation -n production \
  --type=merge --patch '{"spec":{"metric":{"query":"vector(0)"}}}'
echo ""
pause

# ═══════════════════════════════════════════════════════════════════════════════
header "💰 DEMO ACT 3 — FinOps Budget Guard (Feature 3)"
# ═══════════════════════════════════════════════════════════════════════════════

info "ScalePilot tracks your cloud spend in real time."
info "When spend approaches the ceiling, it sends alerts."
info "When it breaches, it delays or blocks scale-up events."
echo ""

if kubectl get scalingbudget test-budget -n scalepilot-system &>/dev/null; then
  step "Current ScalingBudget status:"
  kubectl get scalingbudget test-budget -n scalepilot-system
  echo ""
  kubectl describe scalingbudget test-budget -n scalepilot-system | grep -A20 "Status:"
else
  info "ScalingBudget not deployed (GCP SA key creation was blocked by org policy)."
  info "In a personal GCP project, you'd see:"
  info "  • utilizationPercent climbing in real time"
  info "  • Warning alert at 50% of \$0.10 ceiling"
  info "  • Breach alert + Delay action above \$0.10"
  echo ""
  info "The billing adapter calls AWS Cost Explorer / GCP BigQuery / Azure Cost Mgmt"
  info "and updates status every minute."
fi
echo ""
pause

# ═══════════════════════════════════════════════════════════════════════════════
header "✅ Demo Complete"
# ═══════════════════════════════════════════════════════════════════════════════

echo "  What you just saw:"
echo ""
echo "  ① Predictive Scaling  — ARIMA model pre-warmed HPA before traffic hit"
echo "  ② Federation          — overflow workload distributed across clusters"
echo "  ③ FinOps Budget       — real-time cost enforcement with alerting"
echo ""
echo "  All driven by 3 Kubernetes CRDs:"
echo "    ForecastPolicy         (Feature 1)"
echo "    FederatedScaledObject  (Feature 2)"
echo "    ScalingBudget          (Feature 3)"
echo ""
echo -e "${GREEN}  Grafana dashboard: $GRAFANA_URL${NC}"
echo -e "${GREEN}  Docs:              http://localhost:3001  (npm start in documentation/)${NC}"
echo ""

step "Resetting demo state..."
kubectl scale deployment load-generator -n production --replicas=0 2>/dev/null || true
kubectl patch forecastpolicy web-frontend-forecast -n production \
  --type=merge --patch '{"spec":{"dryRun":true}}' 2>/dev/null || true
kubectl patch federatedscaledobject order-processor-federation -n production \
  --type=merge --patch '{"spec":{"metric":{"query":"vector(5)"}}}' 2>/dev/null || true
echo ""
info "Demo state reset. Ready to run again."
