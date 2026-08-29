#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=install-common.sh
source "$SCRIPT_DIR/install-common.sh"

echo ""
echo -e "${BOLD}GOVA Monolith — Claude Code Setup${NC}"
echo "======================================"

gova_check_prereqs
gova_setup_env "$SCRIPT_DIR"

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

os.makedirs(os.path.dirname(settings_path), exist_ok=True)
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
# install-opencode.sh registers the same two servers in .opencode/opencode.json,
# which is project-scoped -- opencode has no user-scope equivalent of this file.
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

gova_build_containers "$SCRIPT_DIR"
gova_verify_mcp_binary "$CONTAINER_NAME"

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
echo "  Using opencode too? Run ./install-opencode.sh — it shares this .env,"
echo "  these containers, and the same /build and /launch commands."
echo ""
