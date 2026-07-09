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
ok "docker, git, curl present"

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
import json, os

settings_path = os.path.expanduser("~/.claude/settings.json")
try:
    with open(settings_path) as f:
        settings = json.load(f)
except (FileNotFoundError, json.JSONDecodeError):
    settings = {}

settings.setdefault("enabledPlugins", {})
if "superpowers@claude-plugins-official" not in settings["enabledPlugins"]:
    settings["enabledPlugins"]["superpowers@claude-plugins-official"] = True
    print("  + superpowers@claude-plugins-official added")
else:
    print("  - superpowers already registered")

if "mcpServers" in settings:
    del settings["mcpServers"]
    print("  ~ removed stale mcpServers from settings.json")

with open(settings_path, "w") as f:
    json.dump(settings, f, indent=2)
    f.write("\n")
PYEOF

ok "~/.claude/settings.json updated"

step "Registering Stripe MCP"

python3 - <<'PYEOF'
import json, os

claude_json_path = os.path.expanduser("~/.claude.json")
try:
    with open(claude_json_path) as f:
        config = json.load(f)
except (FileNotFoundError, json.JSONDecodeError):
    config = {}

config.setdefault("mcpServers", {})
if "stripe" not in config["mcpServers"]:
    config["mcpServers"]["stripe"] = {"type": "http", "url": "https://mcp.stripe.com/"}
    print("  + stripe MCP registered in ~/.claude.json")
else:
    print("  - stripe MCP already registered")

with open(claude_json_path, "w") as f:
    json.dump(config, f, indent=2)
    f.write("\n")
PYEOF

ok "Stripe MCP registered"

step "Building Docker image"

cd "$SCRIPT_DIR"
docker compose up -d --build
ok "Container up"

step "Verifying MCP server binary"

sleep 2
if docker exec "$CONTAINER_NAME" ls /usr/local/bin/mcp-server >/dev/null 2>&1; then
    ok "MCP server binary present at /usr/local/bin/mcp-server"
else
    fail "MCP server binary not found. Run: docker compose logs app"
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
