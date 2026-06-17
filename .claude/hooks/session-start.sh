#!/bin/bash
# SessionStart hook — install AWS's official agent skills (issue #301).
#
# The "Agent Toolkit for AWS" (https://github.com/aws/agent-toolkit-for-aws)
# ships curated, load-on-demand skills for AWS work: SDK usage, IAM, CloudWatch
# / observability, cost management, networking, containers, S3 and more — all
# directly relevant to this AWS-specific tool. Each Claude Code on the web
# session starts from a fresh container, so the skills are (re)installed here on
# every session start, straight into the project's .claude/skills/ directory
# where Claude Code discovers them.
#
# Best-effort by design: a blocked network, an unreachable registry, or a
# missing npx degrades to "no skills this session" with a printed note — it
# must NEVER fail or block the session. We therefore avoid `set -e` and always
# exit 0.
set -uo pipefail

PROJECT_DIR="${CLAUDE_PROJECT_DIR:-$(pwd)}"
SKILLS_DIR="$PROJECT_DIR/.claude/skills"
MARKER="$SKILLS_DIR/.aws-toolkit-installed"

# SessionStart can fire more than once in a container's life (startup, resume,
# clear). The install is the same each time, so skip if we already did it.
if [ -f "$MARKER" ]; then
  echo "AWS agent skills already installed this session — skipping."
  exit 0
fi

if ! command -v npx >/dev/null 2>&1; then
  echo "note: npx not found; skipping AWS agent-toolkit skill install." >&2
  exit 0
fi

echo "Installing AWS agent-toolkit skills into .claude/skills …"
# --copy writes the skill files directly into .claude/skills/<name> (rather than
# symlinking through .agents/, which Claude Code does not read); -y keeps it
# non-interactive; --skill '*' takes the whole toolkit.
if npx -y skills@latest add aws/agent-toolkit-for-aws/skills \
      --skill '*' --agent claude-code --copy -y >/dev/null 2>&1; then
  mkdir -p "$SKILLS_DIR"
  : > "$MARKER"
  count=$(find "$SKILLS_DIR" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')
  echo "Installed ${count} AWS agent skills into .claude/skills."
else
  echo "note: could not install AWS agent-toolkit skills (offline or registry/GitHub unreachable); continuing without them." >&2
fi

exit 0
