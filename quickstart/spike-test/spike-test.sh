#!/usr/bin/env bash
# Spike test demo — shows ScalePilot pre-warming HPA before load hits
set -euo pipefail

NS=production
HPA=spike-app-hpa
LOAD_DEPLOY=spike-load-generator

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

log()  { echo -e "${CYAN}[$(date +%H:%M:%S)]${NC} $*"; }
head() { echo -e "\n${BOLD}${GREEN}=== $* ===${NC}"; }
warn() { echo -e "${YELLOW}[$(date +%H:%M:%S)] $*${NC}"; }

cleanup() {
  echo ""
  warn "Resetting to baseline..."
  kubectl scale deployment/${LOAD_DEPLOY} -n ${NS} --replicas=0 2>/dev/null || true
  kubectl patch forecastpolicy spike-app-forecast -n ${NS} \
    --type=merge -p '{"spec":{"dryRun":true}}' 2>/dev/null || true
  warn "Cleanup done."
}
trap cleanup INT TERM

status_snapshot() {
  echo ""
  echo -e "${BOLD}--- Status at $(date +%H:%M:%S) ---${NC}"
  echo -e "${CYAN}HPA:${NC}"
  kubectl get hpa ${HPA} -n ${NS} \
    --no-headers -o custom-columns="MIN:.spec.minReplicas,DESIRED:.status.desiredReplicas,CURRENT:.status.currentReplicas,MAX:.spec.maxReplicas" 2>/dev/null || true
  echo -e "${CYAN}ForecastPolicy:${NC}"
  kubectl get forecastpolicy spike-app-forecast -n ${NS} \
    --no-headers -o custom-columns="PREDICTED:.status.predictedReplicas,ACTIVE:.status.active,DRYRUN:.spec.dryRun,RETRAINED:.status.lastRetrainTime" 2>/dev/null || true
  echo -e "${CYAN}Load Generator:${NC}"
  kubectl get deployment/${LOAD_DEPLOY} -n ${NS} \
    --no-headers -o custom-columns="REPLICAS:.status.readyReplicas" 2>/dev/null | tr -d ' ' | xargs printf "  Pods running: %s\n" || true
  echo ""
}

# ── Pre-flight ────────────────────────────────────────────────────────────────
head "Pre-flight checks"
kubectl get deployment spike-app -n ${NS} > /dev/null || { echo "spike-app not deployed. Run: kubectl apply -k quickstart/spike-test/"; exit 1; }
kubectl get hpa ${HPA} -n ${NS} > /dev/null || { echo "HPA ${HPA} not found."; exit 1; }
log "All resources present."

# ── ACT 1: Baseline ───────────────────────────────────────────────────────────
head "ACT 1 — Baseline (0 load, ForecastPolicy in dryRun)"
log "Resetting load generator to 0 replicas..."
kubectl scale deployment/${LOAD_DEPLOY} -n ${NS} --replicas=0
kubectl patch forecastpolicy spike-app-forecast -n ${NS} \
  --type=merge -p '{"spec":{"dryRun":true}}'
log "Waiting 30s for system to settle..."
for i in $(seq 1 3); do sleep 10; status_snapshot; done

# ── ACT 2: ScalePilot learns the pattern ─────────────────────────────────────
head "ACT 2 — Train ScalePilot (enable dryRun=false, light load for history)"
warn "Turning on light load (5 generator pods) to build CPU history..."
kubectl scale deployment/${LOAD_DEPLOY} -n ${NS} --replicas=5
kubectl patch forecastpolicy spike-app-forecast -n ${NS} \
  --type=merge -p '{"spec":{"dryRun":false}}'
log "Running for 90s — ScalePilot trains on this CPU pattern..."
for i in $(seq 1 9); do sleep 10; status_snapshot; done

# ── ACT 3: Spike hits ─────────────────────────────────────────────────────────
head "ACT 3 — Spike wave (20 generator pods, high CPU load)"
warn "Launching spike: 20 load-generator pods with load=80000"
kubectl scale deployment/${LOAD_DEPLOY} -n ${NS} --replicas=20
kubectl set env deployment/${LOAD_DEPLOY} -n ${NS} LOAD_PARAM=80000

log "Watching for 120s — ScalePilot should pre-warm minReplicas before HPA reacts..."
for i in $(seq 1 12); do sleep 10; status_snapshot; done

# ── ACT 4: Cool down ──────────────────────────────────────────────────────────
head "ACT 4 — Cool down (load removed)"
warn "Removing all load..."
kubectl scale deployment/${LOAD_DEPLOY} -n ${NS} --replicas=0
kubectl set env deployment/${LOAD_DEPLOY} -n ${NS} LOAD_PARAM=50000

log "Watching scale-down for 90s..."
for i in $(seq 1 9); do sleep 10; status_snapshot; done

head "Spike test complete"
log "What to look for in the results:"
echo "  - ACT 2: ForecastPolicy PREDICTED column rises as CPU builds"
echo "  - ACT 3: HPA MIN REPLICAS pre-warmed by ScalePilot *before* CURRENT rises"
echo "  - ACT 3: HPA DESIRED catches up quickly (no cold-start delay)"
echo "  - ACT 4: Pods scale back down to 1 after load drops"
cleanup
