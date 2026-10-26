#!/usr/bin/env bash
# Claude Code PreToolUse hook: require security review before git commit.
#
# Intercepts Bash tool calls containing "git commit". If the security review
# stamp is missing or stale (older than the newest .go source file), the
# commit is blocked and Claude is told to run the security review first.
#
# The stamp file is created by the security-review command on APPROVE.

set -euo pipefail

INPUT=$(cat)
COMMAND=$(echo "$INPUT" | jq -r '.tool_input.command // ""')
PROJECT_DIR=$(echo "$INPUT" | jq -r '.cwd // "."')

# Only gate git commit commands.
if ! echo "$COMMAND" | grep -qE '\bgit\b.*\bcommit\b'; then
  exit 0
fi

STAMP="$PROJECT_DIR/.claude/security-review-stamp"

# Check if the stamp file exists.
if [ ! -f "$STAMP" ]; then
  jq -n '{
    "hookSpecificOutput": {
      "hookEventName": "PreToolUse",
      "permissionDecision": "deny",
      "permissionDecisionReason": "Security review required before committing. Run /security-review first. The review must pass with APPROVE verdict to create the stamp file."
    }
  }'
  exit 0
fi

# Check if any .go file is newer than the stamp.
STALE=$(find "$PROJECT_DIR" -name '*.go' -newer "$STAMP" -not -path '*/vendor/*' -print -quit 2>/dev/null || true)

if [ -n "$STALE" ]; then
  jq -n --arg file "$STALE" '{
    "hookSpecificOutput": {
      "hookEventName": "PreToolUse",
      "permissionDecision": "deny",
      "permissionDecisionReason": ("Security review stamp is stale — source files changed since last review (e.g. " + $file + "). Run /security-review again before committing.")
    }
  }'
  exit 0
fi

# Stamp is fresh — allow the commit.
exit 0
