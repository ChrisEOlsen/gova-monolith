#!/usr/bin/env bash
# Shared setup steps for the GOVA installers.
#
# `install-claude.sh` and `install-opencode.sh` differ only in which harness
# they configure — prerequisites, `.env`, the Docker build and the MCP binary
# check are identical, and were duplicated between them until this file existed.
# A duplicated installer is a drifting installer: the APP_NAME normalisation
# below was added to one copy after a real failure, and a second copy would
# have silently kept the bug.
#
# Source this file; do not execute it. It defines functions and sets
# APP_NAME / CONTAINER_NAME / ENV_FILE in the caller's shell.

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BOLD='\033[1m'
NC='\033[0m'

ok()   { echo -e "  ${GREEN}✓${NC} $1"; }
warn() { echo -e "  ${YELLOW}!${NC} $1"; }
fail() { echo -e "  ${RED}✗${NC} $1"; exit 1; }
step() { echo -e "\n${BOLD}▶ $1${NC}"; }

# gova_check_prereqs — everything both installers need before touching anything.
gova_check_prereqs() {
    step "Checking prerequisites"
    command -v docker >/dev/null 2>&1 || fail "docker not found — install Docker Desktop"
    command -v git    >/dev/null 2>&1 || fail "git not found"
    command -v curl   >/dev/null 2>&1 || fail "curl not found"
    command -v python3 >/dev/null 2>&1 || fail "python3 not found — used to edit .env and the harness config"
    # openssl mints SESSION_SECRET below. It was not checked here, so a machine
    # without it failed mid-run under `set -e` with no explanation.
    command -v openssl >/dev/null 2>&1 || fail "openssl not found — needed to generate SESSION_SECRET"
    ok "docker, git, curl, python3, openssl present"

    command -v stripe >/dev/null 2>&1 \
        && ok "stripe CLI present" \
        || warn "stripe CLI not found — install for local webhook testing: https://stripe.com/docs/stripe-cli"
}

# gova_set_env_var FILE KEY VALUE — rewrite one KEY=... line in place.
gova_set_env_var() {
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

# gova_setup_env SCRIPT_DIR — create/refresh .env, prompt for APP_NAME, mint
# SESSION_SECRET. Exports APP_NAME, CONTAINER_NAME and ENV_FILE.
gova_setup_env() {
    local script_dir="$1"
    step "Setting up .env"

    ENV_FILE="$script_dir/.env"
    local example_file="$script_dir/env.example"

    if [ ! -f "$ENV_FILE" ]; then
        cp "$example_file" "$ENV_FILE"
        ok "Copied env.example → .env"
    else
        ok ".env already exists"
    fi

    local current_app_name input_app_name normalized_app_name
    current_app_name=$(grep -E '^APP_NAME=' "$ENV_FILE" | head -1 | cut -d= -f2 | tr -d '"' | tr -d "'")
    current_app_name="${current_app_name:-my-gova-app}"
    printf "  App name [%s]: " "$current_app_name"
    read -r input_app_name </dev/tty
    APP_NAME="${input_app_name:-$current_app_name}"

    # APP_NAME is not just a label: docker-compose.yml uses it as the compose
    # project name (`name: ${APP_NAME:-my-gova-app}`), which is what every
    # container is named after and what CONTAINER_NAME below is built from.
    # Compose only accepts lowercase letters, digits, dash and underscore, so a
    # natural answer like "Task Manager" made `docker compose up` fail several
    # steps later with an error that pointed nowhere near this prompt.
    # Normalise it here instead.
    normalized_app_name=$(printf '%s' "$APP_NAME" \
        | tr '[:upper:]' '[:lower:]' \
        | sed -e 's/[^a-z0-9_-]\{1,\}/-/g' -e 's/^[^a-z0-9]*//' -e 's/[-_]*$//')
    if [ -z "$normalized_app_name" ]; then
        fail "App name must contain at least one letter or digit"
    fi
    if [ "$normalized_app_name" != "$APP_NAME" ]; then
        warn "App name normalised for Docker: '$APP_NAME' → '$normalized_app_name'"
        APP_NAME="$normalized_app_name"
    fi
    gova_set_env_var "$ENV_FILE" "APP_NAME" "$APP_NAME"
    ok "APP_NAME set to: $APP_NAME"

    local current_secret session_secret
    current_secret=$(grep -E '^SESSION_SECRET=' "$ENV_FILE" | head -1 | cut -d= -f2 | tr -d '"' | tr -d "'")
    if [ "$current_secret" = "change-me-to-32-random-bytes-before-use" ] || [ -z "$current_secret" ]; then
        session_secret=$(openssl rand -hex 32)
        gova_set_env_var "$ENV_FILE" "SESSION_SECRET" "$session_secret"
        ok "SESSION_SECRET generated and written to .env"
    else
        ok "SESSION_SECRET already set"
    fi

    CONTAINER_NAME="${APP_NAME}-mcp-1"
    ok "MCP container: $CONTAINER_NAME"
}

# gova_build_containers SCRIPT_DIR — build and start app + mcp.
gova_build_containers() {
    local script_dir="$1"
    step "Building Docker image"
    (cd "$script_dir" && docker compose up -d --build)
    ok "Container up"
}

# gova_verify_mcp_binary CONTAINER — the mcp image embeds its templates at build
# time, so a missing binary here means the build, not the runtime, is broken.
gova_verify_mcp_binary() {
    local container="$1"
    step "Verifying MCP server binary"
    sleep 2
    if docker exec "$container" /usr/local/bin/mcp-server </dev/null >/dev/null 2>&1; then
        ok "MCP server binary present at /usr/local/bin/mcp-server"
    else
        fail "MCP server binary not found. Run: docker compose logs mcp"
    fi
}
