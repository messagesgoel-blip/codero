#!/usr/bin/env bash
set -euo pipefail

owner="messagesgoel-blip"
days=10
cap_minutes=2000
run_limit=100
repos=()

usage() {
  cat <<'EOF'
Usage: github-actions-usage.sh [--owner OWNER] [--days N] [--cap-minutes N] [--limit N] [--repo REPO]

Summarize billed GitHub Actions usage for the current month and recent workflow
run volume for the selected repositories.

Examples:
  scripts/automation/github-actions-usage.sh
  scripts/automation/github-actions-usage.sh --repo codero --repo whimsy
  scripts/automation/github-actions-usage.sh --days 7 --cap-minutes 2000
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --owner)
      owner="$2"
      shift 2
      ;;
    --days)
      days="$2"
      shift 2
      ;;
    --cap-minutes)
      cap_minutes="$2"
      shift 2
      ;;
    --limit)
      run_limit="$2"
      shift 2
      ;;
    --repo)
      repos+=("$2")
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ ${#repos[@]} -eq 0 ]]; then
  repos=(codero whimsy)
fi

command -v gh >/dev/null || { echo "gh CLI is required" >&2; exit 1; }
command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }

month_prefix="$(date -u +%Y-%m-)"
now_epoch="$(date -u +%s)"
cutoff_epoch="$((now_epoch - days * 86400))"

usage_json="$(gh api "users/$owner/settings/billing/usage")"

echo "GitHub Actions usage for $owner"
echo "Current month prefix: ${month_prefix}01"
echo "Budget cap: ${cap_minutes} private-repo Linux minutes"
echo

monthly_private_minutes="$(
  jq --arg month_prefix "$month_prefix" '
    [.usageItems[]
      | select(.date | startswith($month_prefix))
      | select(.product == "actions" and .sku == "Actions Linux")
      | .quantity] | add // 0
  ' <<<"$usage_json"
)"

monthly_private_storage="$(
  jq --arg month_prefix "$month_prefix" '
    [.usageItems[]
      | select(.date | startswith($month_prefix))
      | select(.product == "actions" and .sku == "Actions storage")
      | .quantity] | add // 0
  ' <<<"$usage_json"
)"

cap_pct="$(
  jq -nr --argjson used "$monthly_private_minutes" --argjson cap "$cap_minutes" '
    if $cap == 0 then 0 else (($used / $cap) * 100) end | floor
  '
)"

echo "Current billed private-repo Linux minutes: ${monthly_private_minutes} / ${cap_minutes} (${cap_pct}%)"
echo "Current billed private-repo Actions storage GB-hours: ${monthly_private_storage}"
if jq -e --arg month_prefix "$month_prefix" '.usageItems[] | select(.date | startswith($month_prefix)) | select(.product == "actions" and .sku == "Actions Linux")' >/dev/null <<<"$usage_json"; then
  echo "Billed repositories this month:"
  jq -r --arg month_prefix "$month_prefix" '
    [.usageItems[]
      | select(.date | startswith($month_prefix))
      | select(.product == "actions" and .sku == "Actions Linux")]
    | sort_by(.repositoryName)
    | .[]
    | "  - \(.repositoryName): \(.quantity) minutes"
  ' <<<"$usage_json"
else
  echo "Billed repositories this month: none"
fi

echo
echo "Recent workflow volume (${days}d window, up to ${run_limit} runs per repo):"

for repo in "${repos[@]}"; do
  repo_json="$(gh repo view "$owner/$repo" --json isPrivate,nameWithOwner)"
  visibility="$(jq -r 'if .isPrivate then "private" else "public" end' <<<"$repo_json")"
  echo
  echo "[$owner/$repo] visibility=${visibility}"
  if [[ "$visibility" == "public" ]]; then
    echo "  Public repo note: standard Actions minutes here do not show up against the private-repo billing cap in the billing endpoint."
  fi

  runs_json="$(gh run list -R "$owner/$repo" --limit "$run_limit" --json workflowName,createdAt,event,status,conclusion)"
  recent_count="$(jq --argjson cutoff "$cutoff_epoch" '
    [.[] | select((.createdAt | fromdateiso8601) >= $cutoff)] | length
  ' <<<"$runs_json")"

  echo "  Recent runs captured: ${recent_count}"
  if [[ "$recent_count" == "0" ]]; then
    continue
  fi

  jq -r --argjson cutoff "$cutoff_epoch" '
    [.[] | select((.createdAt | fromdateiso8601) >= $cutoff)]
    | group_by(.workflowName)
    | map({
        workflow: .[0].workflowName,
        count: length,
        events: ([.[].event] | unique | join(",")),
        latest: (map(.createdAt) | max)
      })
    | sort_by(-.count, .workflow)
    | .[]
    | "  - \(.workflow): \(.count) runs, events=\(.events), latest=\(.latest)"
  ' <<<"$runs_json"
done
