---
id: github-actions
title: GitHub Actions CI/CD
sidebar_label: GitHub Actions
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

# GitHub Actions CI/CD Integration

ScalePilot ships with a complete GitHub Actions workflow for linting, testing, building, and deploying the operator. This page explains the existing workflow and shows how to integrate ScalePilot into your own CI/CD pipeline.

## ScalePilot's Own CI Workflow

The repository CI (`.github/workflows/ci.yml`) runs on every push and pull request:

```
push / pull_request
       │
       ├── lint          golangci-lint (24 linters)
       ├── test          go test ./... with -race -cover
       ├── build         go build both binaries
       ├── manifests     verify generated CRD manifests are up to date
       └── ko-build      build + push container image (on main branch only)
```

### Lint Job

```yaml
lint:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with:
        go-version-file: go.mod
        cache: true
    - uses: golangci/golangci-lint-action@v6
      with:
        version: v1.57.2
        args: --timeout=5m
```

### Test Job

```yaml
test:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with:
        go-version-file: go.mod
        cache: true
    - name: Run tests
      run: go test ./... -race -coverprofile=cover.out -timeout 120s
    - name: Upload coverage
      uses: codecov/codecov-action@v4
      with:
        files: cover.out
```

### ko Build + Push

```yaml
ko-build:
  runs-on: ubuntu-latest
  if: github.ref == 'refs/heads/main'
  permissions:
    packages: write
    contents: read
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with:
        go-version-file: go.mod
    - uses: ko-build/setup-ko@v0.6
    - name: Build and push
      env:
        KO_DOCKER_REPO: ghcr.io/${{ github.repository_owner }}/scalepilot
      run: |
        ko build ./cmd/operator/ \
          --tags ${{ github.sha }},latest \
          --platform linux/amd64,linux/arm64
```

## Integrating ScalePilot Into Your Pipeline

### Validate Manifests Before Apply

Use `scalepilot validate` in CI to catch misconfigurations before they reach the cluster:

```yaml title=".github/workflows/deploy.yml"
name: Deploy

on:
  push:
    branches: [main]

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install scalepilot CLI
        run: |
          go install github.com/srujan-rai/scalepilot/cmd/scalepilot@latest
          echo "$HOME/go/bin" >> $GITHUB_PATH

      - name: Validate ScalePilot manifests
        run: scalepilot validate config/manifests/scalepilot/*.yaml

  deploy:
    needs: validate
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Configure kubectl
        uses: azure/k8s-set-context@v4
        with:
          method: kubeconfig
          kubeconfig: ${{ secrets.KUBECONFIG }}

      - name: Deploy ScalePilot manifests
        run: kubectl apply -f config/manifests/scalepilot/
```

### Check Budget Before Deploying

Add a budget health check to prevent deployments that would breach a cost ceiling:

```yaml title=".github/workflows/deploy.yml (with budget check)"
  budget-check:
    runs-on: ubuntu-latest
    steps:
      - name: Install scalepilot CLI
        run: go install github.com/srujan-rai/scalepilot/cmd/scalepilot@latest

      - name: Configure kubectl
        uses: azure/k8s-set-context@v4
        with:
          method: kubeconfig
          kubeconfig: ${{ secrets.KUBECONFIG }}

      - name: Check budget status
        run: |
          output=$(scalepilot budget status --namespace production)
          echo "$output"
          # Fail if any budget is breached
          if echo "$output" | grep -q "YES"; then
            echo "ERROR: A ScalingBudget is in breach state. Deployment blocked."
            exit 1
          fi
```

### Helm Deploy with Values from Secrets

```yaml title=".github/workflows/helm-deploy.yml"
name: Helm Deploy

on:
  release:
    types: [published]

jobs:
  helm-deploy:
    runs-on: ubuntu-latest
    environment: production

    steps:
      - uses: actions/checkout@v4

      - name: Set up Helm
        uses: azure/setup-helm@v4

      - name: Configure kubectl
        uses: azure/k8s-set-context@v4
        with:
          method: kubeconfig
          kubeconfig: ${{ secrets.PROD_KUBECONFIG }}

      - name: Write Helm values
        run: |
          cat > /tmp/values.yaml <<EOF
          image:
            repository: ghcr.io/${{ github.repository_owner }}/scalepilot
            tag: ${{ github.event.release.tag_name }}
          serviceAccount:
            annotations:
              eks.amazonaws.com/role-arn: ${{ secrets.IRSA_ROLE_ARN }}
          prometheusRule:
            enabled: true
          EOF

      - name: Helm upgrade
        run: |
          helm upgrade scalepilot charts/scalepilot \
            --namespace scalepilot-system \
            --create-namespace \
            --values /tmp/values.yaml \
            --wait \
            --timeout 5m

      - name: Verify rollout
        run: |
          kubectl rollout status deployment/scalepilot -n scalepilot-system --timeout=3m
          kubectl get pods -n scalepilot-system
```

### Simulate Forecasts on PR

Run `scalepilot simulate` on pull requests that modify ForecastPolicy manifests, and post results as a PR comment:

```yaml title=".github/workflows/forecast-preview.yml"
name: Forecast Preview

on:
  pull_request:
    paths:
      - 'config/manifests/scalepilot/forecast*.yaml'

jobs:
  simulate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install scalepilot CLI
        run: go install github.com/srujan-rai/scalepilot/cmd/scalepilot@latest

      - name: Configure kubectl (staging cluster)
        uses: azure/k8s-set-context@v4
        with:
          method: kubeconfig
          kubeconfig: ${{ secrets.STAGING_KUBECONFIG }}

      - name: Run simulations
        id: simulate
        run: |
          output=""
          for manifest in config/manifests/scalepilot/forecast*.yaml; do
            name=$(kubectl get -f "$manifest" -o jsonpath='{.metadata.name}' 2>/dev/null || echo "unknown")
            result=$(scalepilot simulate "$name" --namespace staging --horizon 1h --step 5m 2>&1 || true)
            output="$output\n## $name\n\`\`\`\n$result\n\`\`\`\n"
          done
          echo "SIMULATION_OUTPUT<<EOF" >> $GITHUB_OUTPUT
          echo -e "$output" >> $GITHUB_OUTPUT
          echo "EOF" >> $GITHUB_OUTPUT

      - name: Comment on PR
        uses: peter-evans/create-or-update-comment@v4
        with:
          issue-number: ${{ github.event.pull_request.number }}
          body: |
            ## ScalePilot Forecast Simulation Results

            ${{ steps.simulate.outputs.SIMULATION_OUTPUT }}

            Generated by `scalepilot simulate` against the staging cluster.
```

### GitOps with Argo CD

If you use Argo CD, configure ScalePilot as an Application:

```yaml title="argocd-scalepilot-app.yaml"
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: scalepilot
  namespace: argocd
spec:
  project: infrastructure
  source:
    repoURL: https://github.com/srujan-rai/scalepilot.git
    targetRevision: v0.1.0
    path: charts/scalepilot
    helm:
      releaseName: scalepilot
      valueFiles:
        - values-production.yaml
  destination:
    server: https://kubernetes.default.svc
    namespace: scalepilot-system
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
```

## Secrets Management

| Secret | Usage | Recommended Storage |
|--------|-------|---------------------|
| `KUBECONFIG` | kubectl / helm access | GitHub Encrypted Secret |
| `IRSA_ROLE_ARN` | AWS IRSA role ARN | GitHub Encrypted Secret |
| Slack webhook URL | ScalingBudget notifications | Kubernetes Secret (in cluster) |
| Cloud billing credentials | ScalingBudget | Kubernetes Secret (in cluster) |

:::tip Don't Store Cloud Credentials in CI
Instead of storing AWS/GCP/Azure credentials in GitHub Secrets, use IRSA (AWS), Workload Identity (GCP), or Federated Identity (Azure) so the operator pod authenticates using its ServiceAccount token.
:::

## Related Resources

- **[Helm Values Reference](./helm)** - All Helm configuration options
- **[Installation](../getting-started/installation)** - Manual installation guide
- **[Contributing / Development](../contributing/development)** - Local dev setup
