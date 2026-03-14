# Runbook: Webhook Outage / Polling Fallback

## Overview

codero operates in **polling-only mode by default**. Webhooks are an optional
acceleration mechanism, not a correctness requirement. This runbook covers:

1. How to detect a webhook outage.
2. How polling-only mode works as the fallback.
3. How to re-enable webhooks after recovery.

---

## Normal Operating Modes

| Mode | `webhook.enabled` | Reconciler interval | Ingestion |
|---|---|---|---|
| Polling-only (default) | `false` | 60 seconds | Reconciler polls GitHub |
| Webhook-accelerated | `true` | 5 minutes (backstop) | Webhooks + reconciler |

**Key principle**: the reconciler loop provides correctness in all modes.
Webhooks reduce latency but are not on the correctness path.

---

## Config Flags

### YAML (`codero.yaml`)

```yaml
webhook:
  enabled: false         # default: polling-only mode
  port: 9090             # webhook receiver port
  secret: ""             # HMAC-SHA256 secret for signature verification
  path: /webhook/github  # receiver path
```

### Environment Overrides

| Variable | Effect |
|---|---|
| `CODERO_WEBHOOK_ENABLED=true` | Enable webhook receiver |
| `CODERO_WEBHOOK_SECRET=<secret>` | Set HMAC secret |

---

## Detecting a Webhook Outage

Signs that webhooks are not being delivered:

1. GitHub webhook delivery log shows repeated failures or timeouts.
2. Branch states are stale (not progressing despite PR activity).
3. `/health` endpoint shows `webhook: disabled` or `webhook: error`.
4. Logs show no `"webhook: event processed"` entries despite recent PR activity.

Reconciler will continue catching drift every 60–300 seconds even if webhooks
are fully down.

---

## Polling-Only Mode (no action required for outage)

If webhooks stop working, **no immediate action is required**. The reconciler
loop continues running at `PollingOnlyInterval` (60 seconds) and will:

- Detect closed PRs and transition to `closed`.
- Detect stale HEADs (force-pushes) and transition to `stale_branch`.
- Detect revoked approvals and revert from `merge_ready`.
- Detect newly merge-ready branches.

The maximum delay for any state correction in polling-only mode is 60 seconds.

---

## Re-enabling Webhooks After Recovery

1. Verify GitHub can reach the webhook endpoint (`curl -X POST <url>`).
2. Re-configure webhook in GitHub if the URL changed.
3. Set `CODERO_WEBHOOK_ENABLED=true` (or update `codero.yaml`).
4. Restart the daemon:
   ```
   systemctl restart codero
   # or: kill -TERM $(cat /var/run/codero/codero.pid) && codero daemon
   ```
5. Verify in logs: `"webhook receiver starting"` and `"webhook: event processed"`.

---

## Webhook Dedup After Recovery

If GitHub retried webhooks during the outage, duplicate deliveries will arrive
after recovery. codero handles this automatically:

1. Redis `SET NX EX 86400` drops known delivery IDs on the hot path.
2. The durable `webhook_deliveries` table provides secondary idempotency.

**No manual intervention is needed** for duplicate deliveries.

---

## Forcing a Reconciliation Cycle

To trigger an immediate reconciliation (e.g., after restoring from backup):

```bash
# Send SIGUSR1 to trigger a reconciliation cycle (future feature).
# For now, restart the daemon; the reconciler runs immediately on start.
kill -TERM $(cat /var/run/codero/codero.pid)
codero daemon --config codero.yaml
```

---

## Related Runbooks

- `redis-outage.md` — Redis restart and state rebuild.
- `lease-expiry-recovery.md` — Handling stuck `cli_reviewing` branches.
