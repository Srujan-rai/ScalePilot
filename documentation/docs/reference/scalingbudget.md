---
id: scalingbudget
title: ScalingBudget CRD Reference
sidebar_label: ScalingBudget
---

# ScalingBudget CRD Reference

**API Group:** `autoscaling.scalepilot.io/v1alpha1`  
**Kind:** `ScalingBudget`  
**Scope:** Namespaced (typically placed in `scalepilot-system`)

`ScalingBudget` defines a monthly cost ceiling for a target namespace. It polls cloud billing APIs, computes spend utilization, and enforces a breach action (Delay, Downgrade, or Block) when spend exceeds the ceiling.

## Full Example

```yaml
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
    region: us-east-1
    accountId: "123456789012"
  breachAction: Delay
  warningThresholdPercent: 80
  pollIntervalMinutes: 5
  notifications:
    slack:
      webhookURL: https://hooks.slack.com/services/T.../B.../...
      channel: "#finops-alerts"
    pagerDuty:
      routingKey: "your-routing-key"
      severity: warning
```

## Spec Reference

### Top-Level Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `namespace` | `string` | Yes | - | The Kubernetes namespace whose costs are tracked and controlled |
| `ceilingMillidollars` | `int64` | Yes | - | Monthly cost ceiling in thousandths of a dollar (e.g. `150000` = $150.00). Integer avoids floating-point rounding errors. |
| `cloudCost` | `CloudCostConfig` | Yes | - | Cloud billing API integration configuration |
| `breachAction` | `Downgrade\|Delay\|Block` | No | `Delay` | What to do when spend exceeds the ceiling |
| `warningThresholdPercent` | `integer` | No | `80` | Send a warning notification when spend reaches this percentage of the ceiling |
| `pollIntervalMinutes` | `integer` | No | `5` | How often to query the cloud billing API (minimum: 1) |
| `notifications` | `NotificationConfig` | No | - | Alert channels for warning and breach events |

### `cloudCost` - `CloudCostConfig`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `provider` | `AWS\|GCP\|Azure` | Yes | Cloud billing API to use |
| `credentialsSecretRef` | `SecretReference` | Yes | Secret containing cloud API credentials |
| `region` | `string` | No | Filters cost data to a specific cloud region |
| `accountId` | `string` | No | Cloud account or project ID for additional filtering |

#### Credential Secret Formats

**AWS** - keys: `aws_access_key_id`, `aws_secret_access_key`

```yaml
stringData:
  aws_access_key_id: AKIA...
  aws_secret_access_key: ...
```

**GCP** - key: `service_account_json`

```yaml
stringData:
  service_account_json: |
    { "type": "service_account", ... }
```

**Azure** - keys: `tenant_id`, `client_id`, `client_secret`, `subscription_id`

```yaml
stringData:
  tenant_id: ...
  client_id: ...
  client_secret: ...
  subscription_id: ...
```

### `breachAction` Values

| Value | Behavior |
|-------|----------|
| `Delay` | Pauses all scale-up operations in the target namespace. Existing replicas continue running. Scale-ups are queued and blocked until the next billing period resets. |
| `Downgrade` | Allows scaling but reduces CPU/memory requests for new pods. The reduction percentage is determined by the operator configuration. Existing pods are not affected. |
| `Block` | Completely rejects new scale-up events via a validating admission webhook. Any attempt to increase replica counts returns a webhook error. |

### `notifications` - `NotificationConfig`

Both Slack and PagerDuty can be configured simultaneously:

```yaml
notifications:
  slack:
    webhookURL: https://hooks.slack.com/services/T.../B.../...
    channel: "#finops-alerts"      # optional: overrides webhook default channel
  pagerDuty:
    routingKey: "your-routing-key"
    severity: warning              # critical | error | warning | info
```

#### `SlackNotification`

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `webhookURL` | `string` | Yes | Slack incoming webhook URL |
| `channel` | `string` | No | Override channel (e.g. `#alerts`); defaults to webhook channel |

#### `PagerDutyNotification`

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `routingKey` | `string` | Yes | - | PagerDuty Events API v2 routing key |
| `severity` | `string` | No | `warning` | PagerDuty severity: `critical`, `error`, `warning`, or `info` |

## Status Reference

### `status` Fields

| Field | Type | Description |
|-------|------|-------------|
| `currentSpendMillidollars` | `int64` | Current month-to-date spend in millidollars |
| `utilizationPercent` | `integer` | `currentSpend / ceiling * 100` (0–100+) |
| `breached` | `bool` | `true` when current spend ≥ ceiling |
| `lastCheckedAt` | `metav1.Time` | Timestamp of the most recent billing API poll |
| `blockedScaleEvents` | `int32` | Running count of scale-up events blocked by breach enforcement |
| `conditions` | `[]metav1.Condition` | Standard Kubernetes conditions |

### `status.conditions`

| Type | Status | Reason | Description |
|------|--------|--------|-------------|
| `BudgetHealthy` | `True` | `UnderCeiling` | Spend is below the ceiling |
| `BudgetHealthy` | `False` | `CeilingExceeded` | Spend has exceeded the ceiling; breach action is active |
| `WarningThresholdReached` | `True` | `WarningThreshold` | Spend has crossed the warning threshold |
| `WarningThresholdReached` | `False` | `UnderWarningThreshold` | Spend is below the warning threshold |
| `BillingAPIReachable` | `True` | `Reachable` | Cloud billing API is responding |
| `BillingAPIReachable` | `False` | `APIError` | Cloud billing API returned an error; last known value is used |

## kubectl Print Columns

```bash
kubectl get scalingbudgets -n scalepilot-system
# NAME                NAMESPACE    PROVIDER  UTILIZATION  BREACHED  ACTION  AGE
# production-budget   production   AWS       76%          false     Delay   2d
```

| Column | JSON Path | Description |
|--------|----------|-------------|
| `NAMESPACE` | `.spec.namespace` | Target namespace being budgeted |
| `PROVIDER` | `.spec.cloudCost.provider` | Cloud billing provider |
| `UTILIZATION` | `.status.utilizationPercent` | Spend as % of ceiling |
| `BREACHED` | `.status.breached` | Whether ceiling is exceeded |
| `ACTION` | `.spec.breachAction` | Configured breach action |
| `AGE` | `.metadata.creationTimestamp` | Resource age |

## Cost Precision Note

The `ceilingMillidollars` field uses `int64` millidollars (thousandths of a dollar) to avoid floating-point rounding errors when computing utilization percentages:

```
$150.00  → ceilingMillidollars: 150000
$0.50    → ceilingMillidollars: 500
$1500.00 → ceilingMillidollars: 1500000
```

`utilizationPercent = currentSpendMillidollars * 100 / ceilingMillidollars`

## Validation Rules

- `ceilingMillidollars` must be ≥ 1
- `warningThresholdPercent` must be 1–100
- `pollIntervalMinutes` must be ≥ 1
- `provider` must be one of: `AWS`, `GCP`, `Azure`
- `pagerDuty.severity` must be one of: `critical`, `error`, `warning`, `info`

## Related

- **[FinOps Budgets Feature Guide](../features/finops-budgets)**
- **[CLI: scalepilot budget status](../cli/reference#scalepilot-budget-status)**
