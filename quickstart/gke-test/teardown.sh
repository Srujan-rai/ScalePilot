#!/usr/bin/env bash
# ScalePilot GKE Test Teardown — deletes all resources and stops billing.
# Usage: ./teardown.sh <gcp-project-id>

set -euo pipefail

PROJECT_ID="${1:?Usage: ./teardown.sh <gcp-project-id>}"
CLUSTER_NAME="scalepilot-test"
REGION="us-central1"
SA_EMAIL="scalepilot-cost@${PROJECT_ID}.iam.gserviceaccount.com"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }

gcloud config set project "$PROJECT_ID"

info "Deleting GKE cluster $CLUSTER_NAME (stops all compute billing)..."
gcloud container clusters delete "$CLUSTER_NAME" \
  --region "$REGION" \
  --quiet || warn "Cluster not found — already deleted."

info "Deleting service account $SA_EMAIL..."
gcloud iam service-accounts delete "$SA_EMAIL" --quiet || warn "SA not found."

info "Removing local SA key..."
rm -f "$HOME/scalepilot-sa.json"

info "Resetting patched project ID in manifest..."
BUDGET_MANIFEST="$(dirname "$0")/feature3-scalingbudget.yaml"
sed -i "s/${PROJECT_ID}/YOUR_PROJECT_ID/g" "$BUDGET_MANIFEST"

echo ""
echo -e "${GREEN}Teardown complete. No ongoing GCP charges.${NC}"
