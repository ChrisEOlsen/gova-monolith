#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLAUDE_DIR="$HOME/.claude"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BOLD='\033[1m'
NC='\033[0m'

ok()   { echo -e "  ${GREEN}✓${NC} $1"; }
warn() { echo -e "  ${YELLOW}!${NC} $1"; }
fail() { echo -e "  ${RED}✗${NC} $1"; exit 1; }
step() { echo -e "\n${BOLD}▶ $1${NC}"; }

echo ""
echo -e "${BOLD}GOVA Monolith — Claude Code Setup${NC}"
echo "======================================"

step "Checking prerequisites"
command -v docker >/dev/null 2>&1 || fail "docker not found — install Docker Desktop"
command -v git    >/dev/null 2>&1 || fail "git not found"
command -v curl   >/dev/null 2>&1 || fail "curl not found"
# openssl mints SESSION_SECRET below. It was not checked here, so a machine
# without it failed mid-run under `set -e` with no explanation.
command -v openssl >/dev/null 2>&1 || fail "openssl not found — needed to generate SESSION_SECRET"
ok "docker, git, curl, openssl present"

command -v stripe >/dev/null 2>&1 \
    && ok "stripe CLI present" \
    || warn "stripe CLI not found — install for local webhook testing: https://stripe.com/docs/stripe-cli"

step "Setting up .env"

ENV_FILE="$SCRIPT_DIR/.env"
EXAMPLE_FILE="$SCRIPT_DIR/env.example"

set_env_var() {
    local file="$1" key="$2" value="$3"
    python3 - "$file" "$key" "$value" <<'PYEOF'
import sys
path, key, value = sys.argv[1], sys.argv[2], sys.argv[3]
with open(path) as f:
    lines = f.readlines()
lines = [f"{key}={value}\n" if l.startswith(f"{key}=") else l for l in lines]
with open(path, "w") as f:
    f.writelines(lines)
PYEOF
}

if [ ! -f "$ENV_FILE" ]; then
    cp "$EXAMPLE_FILE" "$ENV_FILE"
    ok "Copied env.example → .env"
else
    ok ".env already exists"
fi

CURRENT_APP_NAME=$(grep -E '^APP_NAME=' "$ENV_FILE" | head -1 | cut -d= -f2 | tr -d '"' | tr -d "'")
CURRENT_APP_NAME="${CURRENT_APP_NAME:-my-gova-app}"
printf "  App name [%s]: " "$CURRENT_APP_NAME"
read -r INPUT_APP_NAME </dev/tty
APP_NAME="${INPUT_APP_NAME:-$CURRENT_APP_NAME}"

# APP_NAME is not just a label: docker-compose.yml uses it as the compose
# project name (`name: ${APP_NAME:-my-gova-app}`), which is what every container
# is named after and what CONTAINER_NAME below is built from. Compose only
# accepts lowercase letters, digits, dash and underscore, so a natural answer
# like "Task Manager" made `docker compose up` fail several steps later with an
# error that pointed nowhere near this prompt. Normalise it here instead.
NORMALIZED_APP_NAME=$(printf '%s' "$APP_NAME" \
    | tr '[:upper:]' '[:lower:]' \
    | sed -e 's/[^a-z0-9_-]\{1,\}/-/g' -e 's/^[^a-z0-9]*//' -e 's/[-_]*$//')
if [ -z "$NORMALIZED_APP_NAME" ]; then
    fail "App name must contain at least one letter or digit"
fi
if [ "$NORMALIZED_APP_NAME" != "$APP_NAME" ]; then
    warn "App name normalised for Docker: '$APP_NAME' → '$NORMALIZED_APP_NAME'"
    APP_NAME="$NORMALIZED_APP_NAME"
fi
set_env_var "$ENV_FILE" "APP_NAME" "$APP_NAME"
ok "APP_NAME set to: $APP_NAME"

CURRENT_SECRET=$(grep -E '^SESSION_SECRET=' "$ENV_FILE" | head -1 | cut -d= -f2 | tr -d '"' | tr -d "'")
if [ "$CURRENT_SECRET" = "change-me-to-32-random-bytes-before-use" ] || [ -z "$CURRENT_SECRET" ]; then
    SESSION_SECRET=$(openssl rand -hex 32)
    set_env_var "$ENV_FILE" "SESSION_SECRET" "$SESSION_SECRET"
    ok "SESSION_SECRET generated and written to .env"
else
    ok "SESSION_SECRET already set"
fi

CONTAINER_NAME="${APP_NAME}-mcp-1"
ok "MCP container: $CONTAINER_NAME"

step "Configuring ~/.claude/settings.json"

python3 - <<'PYEOF'
import json, os, sys

settings_path = os.path.expanduser("~/.claude/settings.json")
settings = {}
if os.path.exists(settings_path):
    try:
        with open(settings_path) as f:
            settings = json.load(f)
    except json.JSONDecodeError as e:
        # Abort rather than overwrite -- see the same guard in the MCP step.
        # Falling back to {} here would have replaced the user's global Claude
        # settings (permissions, hooks, env, model) with an empty object because
        # the file had a typo in it.
        sys.exit(
            f"  x ~/.claude/settings.json is not valid JSON ({e}).\n"
            f"    Refusing to overwrite it - fix or move the file, then re-run."
        )

if "mcpServers" in settings:
    del settings["mcpServers"]
    print("  ~ removed stale mcpServers from settings.json")

# This installer used to register the third-party ui-ux-pro-max plugin. It no
# longer does -- UI work is driven by the design bar in .claude/commands/build.md
# instead. Remove the stale entries so a machine that ran an older version of
# this script doesn't keep the plugin enabled.
if settings.get("extraKnownMarketplaces", {}).pop("ui-ux-pro-max-skill", None) is not None:
    print("  ~ removed stale ui-ux-pro-max-skill marketplace")
if settings.get("enabledPlugins", {}).pop("ui-ux-pro-max@ui-ux-pro-max-skill", None) is not None:
    print("  ~ removed stale ui-ux-pro-max plugin")

with open(settings_path, "w") as f:
    json.dump(settings, f, indent=2)
    f.write("\n")
PYEOF

ok "~/.claude/settings.json updated"

step "Registering remote MCP servers"

python3 - <<'PYEOF'
import json, os, sys

# The remote MCP servers a GOVA build expects to be able to reach.
#   stripe   - /build Step 5b uses it when SEED.md checks Payments.
#   context7 - /build Step 5 tells every subagent to look up external API docs
#              with it. It used to be named there and registered nowhere, so an
#              agent that followed the instruction reached for a tool that did
#              not exist.
# These go in ~/.claude.json (user scope). The project's own .mcp.json is
# generated further down for gova-builder and is rewritten per project.
REMOTE_SERVERS = {
    "stripe": {"type": "http", "url": "https://mcp.stripe.com/"},
    "context7": {"type": "http", "url": "https://mcp.context7.com/mcp"},
}

claude_json_path = os.path.expanduser("~/.claude.json")
config = {}
if os.path.exists(claude_json_path):
    try:
        with open(claude_json_path) as f:
            config = json.load(f)
    except json.JSONDecodeError as e:
        # ABORT RATHER THAN OVERWRITE. This used to fall back to config = {} and
        # then write that back out, which turned "your file has a typo in it"
        # into "your entire Claude configuration is gone" - projects, history,
        # every other MCP server. A file we cannot parse is a file we must not
        # replace.
        sys.exit(
            f"  x ~/.claude.json is not valid JSON ({e}).\n"
            f"    Refusing to overwrite it - fix or move the file, then re-run."
        )

config.setdefault("mcpServers", {})
for name, spec in REMOTE_SERVERS.items():
    if name not in config["mcpServers"]:
        config["mcpServers"][name] = spec
        print(f"  + {name} MCP registered in ~/.claude.json")
    else:
        print(f"  - {name} MCP already registered")

with open(claude_json_path, "w") as f:
    json.dump(config, f, indent=2)
    f.write("\n")
PYEOF

ok "Remote MCP servers registered"

step "Building Docker image"

cd "$SCRIPT_DIR"
docker compose up -d --build
ok "Container up"

step "Verifying MCP server binary"

sleep 2
if docker exec "$CONTAINER_NAME" /usr/local/bin/mcp-server </dev/null >/dev/null 2>&1; then
    ok "MCP server binary present at /usr/local/bin/mcp-server"
else
    fail "MCP server binary not found. Run: docker compose logs mcp"
fi

step "Generating .mcp.json"

python3 - "$CONTAINER_NAME" "$SCRIPT_DIR" <<'PYEOF'
import json, sys, os

container   = sys.argv[1]
project_dir = sys.argv[2]
mcp_path    = os.path.join(project_dir, ".mcp.json")

config = {
    "mcpServers": {
        "gova-builder": {
            "command": "docker",
            "args": ["exec", "-i", container, "/usr/local/bin/mcp-server"]
        }
    }
}

with open(mcp_path, "w") as f:
    json.dump(config, f, indent=2)
    f.write("\n")

print(f"  + .mcp.json → gova-builder via {container}")
PYEOF

ok ".mcp.json generated"

echo ""
echo "======================================"
echo -e "${GREEN}${BOLD}Setup complete!${NC}"
echo ""
echo "  1. Fill in SEED.md with your app idea"
echo "  2. Add API keys to .env if needed"
echo "  3. Open Claude Code:  claude"
echo "  4. Verify MCP tools:  /mcp"
echo "  5. Start building:    /build"
echo ""
