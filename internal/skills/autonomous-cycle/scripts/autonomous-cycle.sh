#!/usr/bin/env bash
# autonomous-cycle.sh — overnight autonomous refactor driver for rallish
#
# Usage:
#   nohup ~/.claude/scripts/autonomous-cycle.sh > tmp/nightly.log 2>&1 &
#
# Requirements:
#   - rallish daemon running (or this script will start it)
#   - git repo with clean status or WIP committed
#   - feature branch (not main)

set -euo pipefail

LOG_DIR="${LOG_DIR:-tmp}"
mkdir -p "$LOG_DIR"

LOG_FILE="$LOG_DIR/autonomous-$(date +%Y%m%d-%H%M).log"

echo "🌙 Starting autonomous cycle at $(date)" | tee -a "$LOG_FILE"

# Ensure we're on a feature branch
BRANCH=$(git branch --show-current 2>/dev/null || echo "unknown")
if [ "$BRANCH" = "main" ] || [ "$BRANCH" = "master" ]; then
    echo "❌ Refusing to run on $BRANCH branch. Create a feature branch first." | tee -a "$LOG_FILE"
    exit 1
fi

# Ensure daemon is running
if ! pgrep -f "rallish daemon" >/dev/null 2>&1; then
    echo "🚀 Starting rallish daemon..." | tee -a "$LOG_FILE"
    nohup rallish daemon >> "$LOG_FILE" 2>&1 &
    sleep 3
fi

# Start cycle in background (watch streams to log file)
echo "▶️  Starting cycle..." | tee -a "$LOG_FILE"
rallish cycle start \
    --goal "feat: autonomous-cycle run" \
    --branch "$BRANCH" \
    --max-cycles 10 \
    --max-duration 240 \
    --auto-goal \
    --log-file "$LOG_FILE" \
    >> "$LOG_FILE" 2>&1 &

CYCLE_PID=$!
echo "  pid=$CYCLE_PID  log=$LOG_FILE" | tee -a "$LOG_FILE"

# Trap signals to halt cycle gracefully
trap 'echo "[$CYCLE_PID] graceful halt..."; kill "$CYCLE_PID" 2>/dev/null || true; exit 0' INT TERM

# Monitor loop: wait for cycle process, then check status
wait "$CYCLE_PID" || true

# Morning report
echo "" | tee -a "$LOG_FILE"
echo "🌅 Cycle ended at $(date)" | tee -a "$LOG_FILE"
echo "Log:    $LOG_FILE" | tee -a "$LOG_FILE"
echo "Git log:" | tee -a "$LOG_FILE"
git log --oneline -10 | tee -a "$LOG_FILE"
