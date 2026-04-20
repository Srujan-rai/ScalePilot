#!/usr/bin/env bash
# ScalePilot Repeating Demo Simulation
# Runs a 5-minute cycle continuously so you can record at any point.
# Cycle: Baseline → Traffic Spike → Federation Overflow → Cool Down → repeat
#
# Usage: ./simulate.sh
# Stop:  Ctrl+C  (resets to baseline automatically)

set -euo pipefail

GREEN='\033[0;32m'; BLUE='\033[0;34m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; MAGENTA='\033[0;35m'; BOLD='\033[1m'; NC='\033[0m'

NS="production"
PHASE1_END=60    # 0:00–1:00  baseline
PHASE2_END=150   # 1:00–2:30  traffic spike + predictive scaling
PHASE3_END=240   # 2:30–4:00  federation overflow
PHASE4_END=300   # 4:00–5:00  cool down

banner() {
  clear
  echo -e "${BOLD}${BLUE}╔═══════════════════════════════════════════════════════╗${NC}"
  echo -e "${BOLD}${BLUE}║          ScalePilot Live Demo Simulation              ║${NC}"
  echo -e "${BOLD}${BLUE}╚═══════════════════════════════════════════════════════╝${NC}"
}

progress_bar() {
  local elapsed=$1 total=$2
  local pct=$(( elapsed * 100 / total ))
  local filled=$(( pct / 5 ))
  local bar=""
  for ((i=0; i<20; i++)); do
    [ $i -lt $filled ] && bar+="█" || bar+="░"
  done
  printf "  Cycle progress: [%s] %ds / %ds\n" "$bar" "$elapsed" "$total"
}

status_snapshot() {
  echo ""
  echo -e "${CYAN}  📊 Live Status:${NC}"
  printf "  %-32s %s\n" "HPA current / desired replicas:" \
    "$(kubectl get hpa web-frontend-hpa -n $NS --no-headers \
       -o custom-columns='C:.status.currentReplicas,D:.status.desiredReplicas' 2>/dev/null || echo '—')"
  printf "  %-32s %s\n" "HPA min replicas (set by forecast):" \
    "$(kubectl get hpa web-frontend-hpa -n $NS --no-headers \
       -o custom-columns='M:.spec.minReplicas' 2>/dev/null || echo '—')"
  printf "  %-32s %s\n" "Load generator pods:" \
    "$(kubectl get deployment load-generator -n $NS --no-headers \
       -o custom-columns='R:.spec.replicas' 2>/dev/null || echo '0')"
  printf "  %-32s %s\n" "Overflow replicas:" \
    "$(kubectl get deployment order-processor-overflow -n $NS --no-headers \
       -o custom-columns='R:.spec.replicas' 2>/dev/null || echo '0 (inactive)')"
  echo ""
}

reset_to_baseline() {
  kubectl scale deployment load-generator -n $NS --replicas=0 &>/dev/null || true
  kubectl patch forecastpolicy web-frontend-forecast -n $NS \
    --type=merge --patch '{"spec":{"dryRun":false}}' &>/dev/null || true
  kubectl patch federatedscaledobject order-processor-federation -n $NS \
    --type=merge --patch '{"spec":{"metric":{"query":"vector(0)"}}}' &>/dev/null || true
}

cleanup() {
  echo -e "\n\n${YELLOW}Stopping — resetting to baseline...${NC}"
  reset_to_baseline
  echo -e "${GREEN}Done. Grafana: http://localhost:3001/d/scalepilot-demo/scalepilot-demo${NC}"
  exit 0
}
trap cleanup INT TERM

# Ensure predictive scaling is live before we start
kubectl patch forecastpolicy web-frontend-forecast -n $NS \
  --type=merge --patch '{"spec":{"dryRun":false}}' &>/dev/null || true

reset_to_baseline
CYCLE=0

while true; do
  CYCLE=$((CYCLE + 1))
  CYCLE_START=$(date +%s)
  PHASE2_TRIGGERED=false
  PHASE3_TRIGGERED=false
  PHASE4_TRIGGERED=false

  while true; do
    NOW=$(date +%s)
    ELAPSED=$(( NOW - CYCLE_START ))
    [ $ELAPSED -ge $PHASE4_END ] && break

    banner
    echo -e "  ${BOLD}Cycle #${CYCLE}${NC}  |  Grafana → ${CYAN}http://localhost:3001/d/scalepilot-demo/scalepilot-demo${NC}"
    echo ""
    progress_bar $ELAPSED $PHASE4_END
    echo ""

    if [ $ELAPSED -lt $PHASE1_END ]; then
      echo -e "${BOLD}${GREEN}  ● PHASE 1 — Baseline  ($(( PHASE1_END - ELAPSED ))s)${NC}"
      echo -e "  Everything quiet. No load, no overflow."
      echo -e "  ForecastPolicy is trained and watching."
      echo -e "\n  ${YELLOW}🎬 Show: Grafana baseline, explain ScalePilot architecture${NC}"

    elif [ $ELAPSED -lt $PHASE2_END ]; then
      echo -e "${BOLD}${YELLOW}  ● PHASE 2 — Traffic Spike + Predictive Scaling  ($(( PHASE2_END - ELAPSED ))s)${NC}"
      echo -e "  15 load pods hammering web-frontend."
      echo -e "  ARIMA model predicts peak → HPA minReplicas raised proactively."
      echo -e "\n  ${YELLOW}🎬 Show: 'Predictive Scaling' panel — HPA min rising before CPU spikes${NC}"

      if [ "$PHASE2_TRIGGERED" = false ]; then
        echo -e "\n  ${CYAN}→ Ramping up traffic...${NC}"
        kubectl scale deployment load-generator -n $NS --replicas=15 &>/dev/null || true
        PHASE2_TRIGGERED=true
      fi

    elif [ $ELAPSED -lt $PHASE3_END ]; then
      echo -e "${BOLD}${MAGENTA}  ● PHASE 3 — Federation Overflow  ($(( PHASE3_END - ELAPSED ))s)${NC}"
      echo -e "  Metric returns 5, threshold is 0 → overflow = ceil(5) - 1 = 4 replicas."
      echo -e "  Spillover cluster absorbs excess workload automatically."
      echo -e "\n  ${YELLOW}🎬 Show: 'Federation' panel — green (primary) vs red (overflow) split${NC}"

      if [ "$PHASE3_TRIGGERED" = false ]; then
        echo -e "\n  ${CYAN}→ Triggering federation overflow...${NC}"
        kubectl patch federatedscaledobject order-processor-federation -n $NS \
          --type=merge --patch '{"spec":{"metric":{"query":"vector(5)"}}}' &>/dev/null || true
        PHASE3_TRIGGERED=true
      fi

    else
      echo -e "${BOLD}${BLUE}  ● PHASE 4 — Cool Down  ($(( PHASE4_END - ELAPSED ))s)${NC}"
      echo -e "  Load stopped. Overflow removed. Replicas scaling back down."
      echo -e "  HPA stabilization window: ~5 min to fully scale down."
      echo -e "\n  ${YELLOW}🎬 Show: all panels returning to baseline — full cycle complete${NC}"

      if [ "$PHASE4_TRIGGERED" = false ]; then
        echo -e "\n  ${CYAN}→ Cooling down...${NC}"
        kubectl scale deployment load-generator -n $NS --replicas=0 &>/dev/null || true
        kubectl patch federatedscaledobject order-processor-federation -n $NS \
          --type=merge --patch '{"spec":{"metric":{"query":"vector(0)"}}}' &>/dev/null || true
        PHASE4_TRIGGERED=true
      fi
    fi

    status_snapshot
    echo -e "  Press ${BOLD}Ctrl+C${NC} to stop and reset to baseline."
    sleep 5
  done

  echo -e "\n${GREEN}  ✓ Cycle #${CYCLE} complete — starting cycle #$((CYCLE + 1))...${NC}"
  sleep 2
done
