#!/usr/bin/env bash
# snapshot.sh — Helper to keep snapshot discipline.
#
# This script doesn't actually take VM snapshots (that's a host-OS thing
# done in UTM/Parallels/VirtualBox UI). What it does:
#
#   1. Captures the current state in a tagged git commit
#   2. Writes a small log entry tracking when snapshots were taken
#   3. Reminds you to actually take the VM snapshot via the host UI
#
# Usage:
#   ./scripts/snapshot.sh "after-week-1-scaffolding"
#   ./scripts/snapshot.sh "before-major-refactor"

set -euo pipefail

LABEL="${1:-}"
if [[ -z "$LABEL" ]]; then
  echo "Usage: $0 <snapshot-label>" >&2
  echo "Example: $0 after-week-1-scaffolding" >&2
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

SNAPSHOT_LOG=".snapshot-log"
TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "[snapshot] You have uncommitted changes. Commit or stash first." >&2
  git status --short
  exit 1
fi

CURRENT_COMMIT=$(git rev-parse HEAD)
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)

if [[ ! -f "$SNAPSHOT_LOG" ]]; then
  cat > "$SNAPSHOT_LOG" << 'EOF'
# Snapshot log
# Each entry corresponds to a VM snapshot taken via the host hypervisor UI.
# Format: TIMESTAMP | LABEL | BRANCH | COMMIT

EOF
fi

echo "${TIMESTAMP} | ${LABEL} | ${CURRENT_BRANCH} | ${CURRENT_COMMIT}" >> "$SNAPSHOT_LOG"

echo ""
echo "================================================================"
echo " Snapshot checkpoint logged"
echo "================================================================"
echo ""
echo "  Label:  ${LABEL}"
echo "  Branch: ${CURRENT_BRANCH}"
echo "  Commit: ${CURRENT_COMMIT:0:12}"
echo ""
echo "  ⚠️  Now take an actual VM snapshot via your hypervisor UI:"
echo ""
echo "      UTM:        Right-click VM → New Snapshot"
echo "      Parallels:  Actions → Take Snapshot"
echo "      VirtualBox: Machine → Take Snapshot"
echo "      VMware:     VM → Snapshot → Take Snapshot"
echo ""
echo "  Use the same label: ${LABEL}"
echo ""
echo "  Recommended retention: keep last 5 named snapshots + last 7 days"
echo "  of automatic snapshots if your hypervisor supports them."
echo ""
