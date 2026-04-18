#!/usr/bin/env bash
# ScalePilot GKE Test Setup
# Usage: ./setup.sh <gcp-project-id> [sa-key-path]
#
# sa-key-path defaults to ~/scalepilot-sa.json if omitted.
# The script creates the cluster, installs dependencies, wires secrets,
# and drops you into a live watch of all three features.

set -euo pipefail

PROJECT_ID="${1:?Usage: ./setup.sh <gcp-project-id> [sa-key-path]}"
SA_KEY="${2:-$HOME/scalepilot-sa.json}"
CLUSTER_NAME="scalepilot-test"
REGION="us-central1"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
die()   { echo -e "${RED}[ERR]${NC}   $*" >&2; exit 1; }

# ── 1. Prerequisites ──────────────────────────────────────────────────────────
for cmd in gcloud kubectl helm; do
  command -v "$cmd" &>/dev/null || die "$cmd is required but not installed."
done

gcloud config set project "$PROJECT_ID"

# ── 2. GKE Autopilot Cluster ──────────────────────────────────────────────────
if gcloud container clusters describe "$CLUSTER_NAME" --region "$REGION" &>/dev/null; then
  warn "Cluster $CLUSTER_NAME already exists — skipping creation."
else
  info "Creating Autopilot cluster $CLUSTER_NAME (this takes ~5 min)..."
  gcloud container clusters create-auto "$CLUSTER_NAME" \
    --region "$REGION" \
    --release-channel stable \
    --quiet
fi

info "Fetching cluster credentials..."
gcloud container clusters get-credentials "$CLUSTER_NAME" --region "$REGION"

# ── 3. Install CRDs ───────────────────────────────────────────────────────────
info "Installing ScalePilot CRDs..."
make -C "$REPO_ROOT" install

# ── 4. Prometheus (minimal, no Alertmanager/Grafana) ─────────────────────────
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts --force-update &>/dev/null
helm repo update &>/dev/null

if helm status prometheus -n monitoring &>/dev/null; then
  warn "Prometheus already installed — skipping."
else
  info "Installing Prometheus..."
  kubectl create namespace monitoring --dry-run=client -o yaml | kubectl apply -f -
  helm install prometheus prometheus-community/prometheus \
    --namespace monitoring \
    --set alertmanager.enabled=false \
    --set prometheus-pushgateway.enabled=false \
    --set server.persistentVolume.size=2Gi \
    --wait --timeout 5m
fi

# ── 5. GCP Service Account for billing ───────────────────────────────────────
SA_NAME="scalepilot-cost"
SA_EMAIL="${SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"

if ! gcloud iam service-accounts describe "$SA_EMAIL" &>/dev/null; then
  info "Creating GCP service account $SA_EMAIL..."
  gcloud iam service-accounts create "$SA_NAME" \
    --display-name "ScalePilot Cost Reader" --quiet

  gcloud projects add-iam-policy-binding "$PROJECT_ID" \
    --member="serviceAccount:${SA_EMAIL}" \
    --role="roles/bigquery.dataViewer" --quiet

  gcloud projects add-iam-policy-binding "$PROJECT_ID" \
    --member="serviceAccount:${SA_EMAIL}" \
    --role="roles/bigquery.jobUser" --quiet
fi

if [[ ! -f "$SA_KEY" ]]; then
  info "Downloading service account key to $SA_KEY..."
  gcloud iam service-accounts keys create "$SA_KEY" \
    --iam-account="$SA_EMAIL" --quiet
else
  warn "SA key $SA_KEY already exists — reusing it."
fi

# ── 6. Kubernetes Secrets ─────────────────────────────────────────────────────
kubectl create namespace scalepilot-system --dry-run=client -o yaml | kubectl apply -f -

info "Creating GCP billing credentials secret..."
kubectl create secret generic gcp-billing-creds \
  --from-file=service_account_json="$SA_KEY" \
  -n scalepilot-system \
  --dry-run=client -o yaml | kubectl apply -f -

info "Creating cluster kubeconfig secrets for federation..."
KUBECONFIG_PATH="$(mktemp)"
kubectl config view --raw > "$KUBECONFIG_PATH"

kubectl create secret generic cluster-primary \
  --from-file=kubeconfig="$KUBECONFIG_PATH" \
  -n scalepilot-system \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl create secret generic cluster-overflow \
  --from-file=kubeconfig="$KUBECONFIG_PATH" \
  -n scalepilot-system \
  --dry-run=client -o yaml | kubectl apply -f -

rm -f "$KUBECONFIG_PATH"

# ── 7. Patch Project ID into ScalingBudget manifest ──────────────────────────
BUDGET_MANIFEST="$(dirname "$0")/feature3-scalingbudget.yaml"
sed -i "s/YOUR_PROJECT_ID/${PROJECT_ID}/g" "$BUDGET_MANIFEST"
info "Patched project ID into feature3-scalingbudget.yaml"

# ── 8. Apply all manifests ────────────────────────────────────────────────────
info "Applying all ScalePilot test manifests..."
kubectl apply -k "$(dirname "$0")"

# ── 9. Start operator locally ─────────────────────────────────────────────────
info "Setup complete! Starting ScalePilot operator..."
echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}  ScalePilot GKE Test Ready${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo "  Watch Feature 1 (ForecastPolicy):"
echo "    kubectl get forecastpolicy -n production -w"
echo ""
echo "  Watch Feature 2 (Federation):"
echo "    kubectl get federatedscaledobject -n production -w"
echo ""
echo "  Watch Feature 3 (Budget):"
echo "    kubectl get scalingbudget -n scalepilot-system -w"
echo ""
echo "  Operator logs:"
echo "    kubectl logs -n scalepilot-system deploy/scalepilot-controller-manager -f"
echo ""
echo "  When done:"
echo "    ./teardown.sh $PROJECT_ID"
echo ""

# Run the operator (blocks — Ctrl+C to stop)
make -C "$REPO_ROOT" run
