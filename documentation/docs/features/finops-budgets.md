---
id: finops-budgets
title: FinOps ScalingBudget
sidebar_label: FinOps Budgets
---

import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

# FinOps ScalingBudget

`ScalingBudget` defines a namespace-scoped monthly cost ceiling. It polls cloud billing APIs (AWS Cost Explorer, GCP Billing, Azure Cost Management), computes real spend against the ceiling, and enforces one of three breach actions — automatically, without manual intervention.

## How It Works

```
┌───────────────────────────────────────────────────────────┐
│ Every pollIntervalMinutes (default: 5m):                  │
│   1. Queries cloud cost API for namespace spend           │
│      (filtered by k8s namespace cost allocation tag)      │
│   2. Computes utilization = spend / ceiling * 100         │
│   3. If utilization ≥ warningThresholdPercent (def: 80%): │
│      → Sends Slack/PagerDuty warning notification         │
│   4. If spend ≥ ceiling (first breach only):              │
│      → Sends critical breach notification                 │
│      → Activates breach action (Delay/Downgrade/Block)    │
│   5. Updates Status: spend, utilization%, breached flag   │
│      blockedScaleEvents counter                           │
│                                                           │
│ Cost data is cached in-memory with 5-minute TTL to        │
│ avoid hitting cloud billing API rate limits.              │
└───────────────────────────────────────────────────────────┘
```

## Breach Actions

When `status.breached` becomes `true`, ScalePilot enforces one of three actions:

| Action | Behavior | Use When |
|--------|----------|---------|
| `Delay` | Pauses all scale-up events in the namespace until the next billing period. Existing replicas are unaffected. | You want to stop cost growth immediately but can't downgrade resources |
| `Downgrade` | Reduces CPU/memory requests for new pods created by scale-up events. Existing pods remain unchanged. | You want scaling to continue but at lower resource intensity |
| `Block` | Rejects scale-up entirely via a validating admission webhook. New replica creation fails with a budget error. | Zero-tolerance cost ceiling — no scaling allowed when breached |

:::tip Recommended Action for Production
`Delay` is the safest default for most teams. It stops cost growth without affecting running workloads, and automatically lifts when the next billing period starts (typically the 1st of the month).
:::

## Cloud Provider Setup

<Tabs>
<TabItem value="aws" label="AWS Setup">

### Required IAM Permissions

```json title="scalepilot-cost-explorer-policy.json"
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["ce:GetCostAndUsage"],
      "Resource": "*"
    }
  ]
}
```

### Enable Cost Allocation Tags

1. Go to AWS Billing Console → Cost allocation tags
2. Activate the tag `kubernetes-namespace`
3. Apply the tag to your EKS node groups or EC2 instances

Tag your EKS workloads:

```bash
# Tag the EKS managed node group
aws eks tag-resource \
  --resource-arn arn:aws:eks:us-east-1:123456789:nodegroup/my-cluster/workers/... \
  --tags "kubernetes-namespace=production"
```

### Credentials Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: aws-cost-explorer-creds
  namespace: scalepilot-system
type: Opaque
stringData:
  aws_access_key_id: AKIA...
  aws_secret_access_key: your-secret-key
```

</TabItem>
<TabItem value="gcp" label="GCP Setup">

### Required IAM Roles

Grant the service account `roles/billing.viewer` on your GCP project.

```bash
gcloud projects add-iam-policy-binding YOUR_PROJECT_ID \
  --member="serviceAccount:scalepilot@YOUR_PROJECT_ID.iam.gserviceaccount.com" \
  --role="roles/billing.viewer"
```

### Enable BigQuery Billing Export

1. Go to GCP Console → Billing → Billing export
2. Enable **Standard usage cost** export to BigQuery
3. Enable GKE resource-based cost allocation and label your workloads:

```bash
kubectl label namespace production k8s-namespace=production
```

### Credentials Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: gcp-billing-creds
  namespace: scalepilot-system
type: Opaque
stringData:
  service_account_json: |
    {
      "type": "service_account",
      "project_id": "your-project",
      "private_key_id": "...",
      "private_key": "-----BEGIN RSA PRIVATE KEY-----\n...",
      "client_email": "scalepilot@your-project.iam.gserviceaccount.com",
      "client_id": "...",
      "auth_uri": "https://accounts.google.com/o/oauth2/auth",
      "token_uri": "https://oauth2.googleapis.com/token"
    }
```

</TabItem>
<TabItem value="azure" label="Azure Setup">

### Required Role Assignment

Assign `Cost Management Reader` to a service principal:

```bash
az role assignment create \
  --role "Cost Management Reader" \
  --assignee YOUR_CLIENT_ID \
  --scope "/subscriptions/YOUR_SUBSCRIPTION_ID"
```

### Tag Azure Resources

Tag your AKS node pool resources:

```bash
az aks nodepool update \
  --resource-group my-rg \
  --cluster-name my-cluster \
  --name default \
  --tags "kubernetes-namespace=production"
```

### Credentials Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: azure-cost-creds
  namespace: scalepilot-system
type: Opaque
stringData:
  tenant_id: "your-tenant-id"
  client_id: "your-client-id"
  client_secret: "your-client-secret"
  subscription_id: "your-subscription-id"
```

</TabItem>
</Tabs>

## Complete Example

```yaml title="scaling-budget.yaml"
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ScalingBudget
metadata:
  name: production-budget
  namespace: scalepilot-system
spec:
  # Which namespace's costs to track and control
  namespace: production

  # Monthly cost ceiling: $150.00 expressed as millidollars
  # Integer avoids floating-point rounding: 150000 = $150.000
  ceilingMillidollars: 150000

  # Cloud billing integration
  cloudCost:
    provider: AWS               # AWS | GCP | Azure
    credentialsSecretRef:
      name: aws-cost-explorer-creds
      namespace: scalepilot-system
    region: us-east-1
    accountId: "123456789012"   # optional: filter to specific account

  # What to do when the ceiling is breached
  breachAction: Delay           # Downgrade | Delay | Block

  # Send a warning when spend reaches 80% of ceiling ($120)
  warningThresholdPercent: 80

  # How often to poll the billing API (minimum 1 minute)
  pollIntervalMinutes: 5

  # Alert channels
  notifications:
    slack:
      webhookURL: https://hooks.slack.com/services/T.../B.../...
      channel: "#finops-alerts"
    pagerDuty:
      routingKey: "your-pagerduty-routing-key"
      severity: warning         # critical | error | warning | info
```

```bash
kubectl apply -f scaling-budget.yaml
```

## Monitoring Budget Status

```bash
# Using kubectl
kubectl get scalingbudget production-budget -n scalepilot-system
# NAME                NAMESPACE    PROVIDER  UTILIZATION  BREACHED  ACTION  AGE
# production-budget   production   AWS       76%          false     Delay   2d

# Using CLI
scalepilot budget status
```

```
NAMESPACE    NAME                PROVIDER  CEILING   SPEND     UTILIZATION  BREACHED  ACTION  BLOCKED
production   production-budget   AWS       $150.00   $114.50   76%          No        Delay   0
staging      staging-budget      GCP       $50.00    $52.30    104%         YES       Block   3
```

Detailed status:

```bash
kubectl describe scalingbudget production-budget -n scalepilot-system
```

```yaml
Status:
  Current Spend Millidollars: 114500   # $114.50
  Utilization Percent: 76
  Breached: false
  Last Checked At: 2026-04-18T14:35:00Z
  Blocked Scale Events: 0
  Conditions:
    - Type: BudgetHealthy
      Status: "True"
      Reason: UnderCeiling
      Message: Current spend $114.50 is 76% of $150.00 ceiling
    - Type: WarningThresholdReached
      Status: "False"
      Reason: UnderWarningThreshold
```

## Notification Payloads

### Slack Warning Notification

```json
{
  "text": "⚠️ *ScalePilot Budget Warning* — `production` namespace",
  "attachments": [{
    "color": "warning",
    "fields": [
      { "title": "Namespace", "value": "production", "short": true },
      { "title": "Spend", "value": "$120.50 / $150.00", "short": true },
      { "title": "Utilization", "value": "80%", "short": true },
      { "title": "Provider", "value": "AWS", "short": true }
    ]
  }]
}
```

### PagerDuty Breach Event

```json
{
  "routing_key": "your-routing-key",
  "event_action": "trigger",
  "payload": {
    "summary": "ScalingBudget breached: production namespace exceeded $150.00 ceiling",
    "severity": "warning",
    "source": "scalepilot",
    "custom_details": {
      "namespace": "production",
      "ceiling_dollars": "150.00",
      "spend_dollars": "152.30",
      "action": "Delay"
    }
  }
}
```

## Cost Caching

Cloud billing APIs are rate-limited and can take 100–500ms per call. ScalePilot wraps all billing adapters with an in-memory cache:

- **TTL**: 5 minutes (matches `pollIntervalMinutes` default)
- **Scope**: Per-namespace cache entries
- **Thread safety**: `sync.RWMutex` for concurrent ScalingBudget reconcilers

This means billing API calls happen **at most once per namespace per poll interval**, regardless of how many reconcile cycles run.

## Multiple Budgets

You can create multiple `ScalingBudget` resources targeting different namespaces:

```yaml
# Budget for production
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ScalingBudget
metadata:
  name: production-budget
  namespace: scalepilot-system
spec:
  namespace: production
  ceilingMillidollars: 150000
  cloudCost:
    provider: AWS
    credentialsSecretRef:
      name: aws-cost-explorer-creds
      namespace: scalepilot-system
  breachAction: Delay
  warningThresholdPercent: 80
  pollIntervalMinutes: 5
---
# Stricter budget for staging
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ScalingBudget
metadata:
  name: staging-budget
  namespace: scalepilot-system
spec:
  namespace: staging
  ceilingMillidollars: 25000     # $25/month for staging
  cloudCost:
    provider: AWS
    credentialsSecretRef:
      name: aws-cost-explorer-creds
      namespace: scalepilot-system
  breachAction: Block            # Hard block for staging
  warningThresholdPercent: 70    # Warn earlier
  pollIntervalMinutes: 10        # Check less frequently
```

## Related Resources

- **[ScalingBudget CRD Reference](../reference/scalingbudget)** — Full spec and status fields
- **[CLI: budget status](../cli/reference#scalepilot-budget-status)** — Live budget dashboard
- **[GitHub Actions integration](../deployment/github-actions)** — Add budget checks to CI/CD
