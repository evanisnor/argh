#!/usr/bin/env bash
# new-session-on-plan-task-done.sh
#
# PostToolUse hook: fires after every Edit or Write call.
#
# Checks whether plan.yaml was the file modified. If it was, and a task
# with status: done is present, signals autoclaude to start a new session.
# This ends the current Claude session cleanly so autoclaude can restart
# with a fresh context window for the next plan task.

set -euo pipefail

INPUT=$(cat)

# Extract the file path from the tool input (Edit and Write both use file_path)
FILE_PATH=$(printf '%s' "$INPUT" | jq -r '.tool_input.file_path // empty' 2>/dev/null)

# Only act on plan.yaml modifications
[[ "$FILE_PATH" != *"plan.yaml" ]] && exit 0

PLAN_FILE="$PWD/plan.yaml"
[[ ! -f "$PLAN_FILE" ]] && exit 0

# Check if any task is marked done
if grep -qE '^\s+status:\s+done\s*$' "$PLAN_FILE"; then
  mkdir -p "$PWD/.autoclaude"
  touch "$PWD/.autoclaude/new_session_requested"

  printf 'A task in plan.yaml has been marked done. Exit your current turn now — autoclaude will automatically start a new session with a fresh context window for the next plan task.\n'
fi
