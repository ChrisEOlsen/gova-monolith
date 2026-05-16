#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

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
echo -e "${BOLD}GOVA Monolith — OpenCode Setup${NC}"
echo "======================================"

step "Checking prerequisites"
command -v docker   >/dev/null 2>&1 || fail "docker not found"
command -v git      >/dev/null 2>&1 || fail "git not found"
command -v curl     >/dev/null 2>&1 || fail "curl not found"
command -v opencode >/dev/null 2>&1 || fail "opencode not found — install from https://opencode.ai"
ok "docker, git, curl, opencode present"

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
    ok "SESSION_SECRET generated"
else
    ok "SESSION_SECRET already set"
fi

CONTAINER_NAME="${APP_NAME}-app-1"

step "Writing opencode.json"
cat > "$SCRIPT_DIR/opencode.json" <<JSONEOF
{
  "model": "anthropic/claude-opus-4-7"
}
JSONEOF
ok "opencode.json written"

step "Building Docker image"
cd "$SCRIPT_DIR"
docker compose up -d --build
ok "Container up"

step "Verifying MCP server binary"
sleep 2
if docker exec "$CONTAINER_NAME" ls /usr/local/bin/mcp-server >/dev/null 2>&1; then
    ok "MCP server binary present"
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
        },
        "stripe": {
            "type": "http",
            "url": "https://mcp.stripe.com/"
        }
    }
}

with open(mcp_path, "w") as f:
    json.dump(config, f, indent=2)
    f.write("\n")
print(f"  + .mcp.json → gova-builder + stripe via {container}")
PYEOF

ok ".mcp.json generated"

echo ""
echo "======================================"
echo -e "${GREEN}${BOLD}Setup complete!${NC}"
echo ""
echo "  1. Fill in SEED.md with your app idea"
echo "  2. Add API keys to .env if needed"
echo "  3. Open OpenCode:     opencode"
echo "  4. Start building:    /build"
echo ""
