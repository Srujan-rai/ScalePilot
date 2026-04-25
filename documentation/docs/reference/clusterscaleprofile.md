---
id: clusterscaleprofile
title: ClusterScaleProfile CRD Reference
sidebar_label: ClusterScaleProfile
---

# ClusterScaleProfile CRD Reference

**API Group:** `autoscaling.scalepilot.io/v1alpha1`  
**Kind:** `ClusterScaleProfile`  
**Scope:** Cluster-scoped (no namespace)

`ClusterScaleProfile` provides cluster-wide scaling governance. It defines maximum surge percentages, cron-based blackout windows, and per-team RBAC-aware scaling constraints. All ScalePilot reconcilers check the active `ClusterScaleProfile` before applying any scaling action.

## Full Example

```yaml
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ClusterScaleProfile
metadata:
  name: default    # cluster-scoped: no namespace field
spec:
  maxSurgePercent: 25
  defaultCooldownSeconds: 60
  enableGlobalDryRun: false

  blackoutWindows:
    - name: maintenance-friday
      start: "0 22 * * 5"
      end: "0 6 * * 6"
      timezone: America/New_York

    - name: deploy-freeze-eom
      start: "0 0 28 * *"
      end: "0 0 1 * *"
      timezone: UTC

  teamOverrides:
    - teamName: platform-team
      namespaces: [platform, infrastructure]
      maxSurgePercent: 50
      maxReplicasPerDeployment: 100

    - teamName: app-team
      namespaces: [production, staging]
      maxSurgePercent: 25
      allowedAlgorithms: [ARIMA]
      maxReplicasPerDeployment: 50
```

## Spec Reference

### Top-Level Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `maxSurgePercent` | `int32` | No | `25` | Maximum percentage increase in replicas allowed per reconcile cycle, cluster-wide |
| `defaultCooldownSeconds` | `int32` | No | `60` | Minimum seconds between consecutive scale-up events, cluster-wide |
| `enableGlobalDryRun` | `bool` | No | `false` | Disable ALL scaling operations cluster-wide; log intended actions only |
| `blackoutWindows` | `[]BlackoutWindow` | No | - | Time windows during which all scaling is suppressed |
| `teamOverrides` | `[]TeamOverride` | No | - | Per-team constraints that override cluster defaults |

### `BlackoutWindow`

Blackout windows use standard 5-field cron syntax. Scaling is suppressed for any reconcile cycle where the current time falls between a `start` cron evaluation and the corresponding `end` cron evaluation.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | `string` | Yes | - | Human-readable label for this window (e.g. `"maintenance-friday"`) |
| `start` | `string` | Yes | - | Cron expression for when the blackout begins |
| `end` | `string` | Yes | - | Cron expression for when the blackout ends |
| `timezone` | `string` | No | `UTC` | IANA timezone identifier (e.g. `"America/New_York"`, `"Asia/Tokyo"`) |

#### Cron Syntax Reference

ScalePilot uses standard 5-field cron: `minute hour day-of-month month day-of-week`

| Expression | Meaning |
|-----------|---------|
| `0 22 * * 5` | Every Friday at 10:00 PM |
| `0 6 * * 6` | Every Saturday at 6:00 AM |
| `0 0 28 * *` | 28th of every month at midnight |
| `30 2 * * 0,6` | Saturdays and Sundays at 2:30 AM |
| `0 0 1 * *` | First day of every month at midnight |
| `0 9 * * 1-5` | Monday–Friday at 9:00 AM |

#### Blackout Window Examples

```yaml
blackoutWindows:
  # Friday evening maintenance (US Eastern)
  - name: friday-maintenance
    start: "0 22 * * 5"    # Friday 10pm ET
    end: "0 6 * * 6"       # Saturday 6am ET
    timezone: America/New_York

  # End-of-month deployment freeze
  - name: eom-freeze
    start: "0 0 28 * *"    # 28th midnight UTC
    end: "0 0 1 * *"       # 1st midnight UTC (next month)
    timezone: UTC

  # Holiday freeze (specific dates - use with caution)
  - name: new-year-freeze
    start: "0 0 31 12 *"   # Dec 31 midnight
    end: "0 0 2 1 *"       # Jan 2 midnight
    timezone: UTC
```

### `TeamOverride`

Team overrides apply when a scaling operation is associated with a specific team (mapped to a Kubernetes Group or ServiceAccount prefix).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `teamName` | `string` | Yes | Identifies the team (maps to a Kubernetes Group or ServiceAccount name prefix) |
| `namespaces` | `[]string` | Yes | Restricts this override to specific namespaces (minimum: 1) |
| `maxSurgePercent` | `int32` | No | Override max surge % for this team (overrides cluster default) |
| `allowedAlgorithms` | `[]ForecastAlgorithm` | No | If set, restricts which forecast algorithms this team's ForecastPolicies may use |
| `maxReplicasPerDeployment` | `int32` | No | Maximum replicas any single Deployment owned by this team can scale to |

## Status Reference

### `status` Fields

| Field | Type | Description |
|-------|------|-------------|
| `activeBlackout` | `bool` | `true` when the current time falls within any blackout window |
| `activeBlackoutName` | `string` | Name of the currently active blackout window (empty when none active) |
| `teamsConfigured` | `int32` | Count of `teamOverrides` entries |
| `lastReconcileTime` | `metav1.Time` | Timestamp of the most recent reconciliation |
| `conditions` | `[]metav1.Condition` | Standard Kubernetes conditions |

### `status.conditions`

| Type | Status | Reason | Description |
|------|--------|--------|-------------|
| `Ready` | `True` | `Synced` | Profile is evaluated and active |
| `Ready` | `False` | `EvaluationError` | Cron parsing failed for one or more blackout windows |
| `BlackoutActive` | `True` | `<window-name>` | A blackout window is currently active; scaling is suppressed |
| `BlackoutActive` | `False` | `NoActiveBlackout` | No blackout window is currently active |

## kubectl Print Columns

```bash
kubectl get clusterscaleprofiles
# NAME      MAXSURGE  BLACKOUT  TEAMS  DRYRUN  AGE
# default   25%       false     2      false   5d
```

| Column | JSON Path | Description |
|--------|----------|-------------|
| `MAXSURGE` | `.spec.maxSurgePercent` | Maximum surge % per cycle |
| `BLACKOUT` | `.status.activeBlackout` | Whether a blackout window is currently active |
| `TEAMS` | `.status.teamsConfigured` | Number of team overrides |
| `DRYRUN` | `.spec.enableGlobalDryRun` | Global dry-run mode |
| `AGE` | `.metadata.creationTimestamp` | Resource age |

## How Reconcilers Check the Profile

Every ScalePilot reconciler (ForecastPolicy, FederatedScaledObject) checks the `ClusterScaleProfile` before applying any scaling action:

1. **Fetch the profile**: reads the `ClusterScaleProfile` named `default` (or any single existing profile)
2. **Check global dry-run**: if `enableGlobalDryRun: true`, log the intended action and return
3. **Check blackout window**: if `status.activeBlackout: true`, skip the scaling action with reason `ProfileBlocked`
4. **Apply surge limiting**: cap the replica increase to `maxSurgePercent` of current replicas
5. **Apply team overrides**: if the requesting team is in `teamOverrides`, apply team-specific limits

## Validation Rules

- `maxSurgePercent` must be 1–100
- `defaultCooldownSeconds` must be ≥ 0
- `blackoutWindows[*].name` must be ≥ 9 characters (MinLength validation)
- `teamOverrides[*].namespaces` must have at least 1 entry
- `teamOverrides[*].maxSurgePercent`, when set, must be 1–100
- `teamOverrides[*].allowedAlgorithms` values must be `ARIMA` or `HoltWinters`

## Related

- **[ClusterScaleProfile in the Quick Start](../getting-started/quick-start#optional-add-a-clusterscaleprofile)**
- **[Predictive Scaling - how the profile interacts with ForecastPolicy](../features/predictive-scaling)**
