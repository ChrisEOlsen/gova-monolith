#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck source=install-common.sh
source "$SCRIPT_DIR/install-common.sh"

echo ""
echo -e "${BOLD}GOVA Monolith — opencode Setup${NC}"
echo "======================================"

gova_check_prereqs

command -v opencode >/dev/null 2>&1 \
    || fail "opencode not found — install it: curl -fsSL https://opencode.ai/install | bash"
ok "opencode $(opencode --version 2>/dev/null | tail -1) present"

gova_setup_env "$SCRIPT_DIR"
gova_build_containers "$SCRIPT_DIR"
gova_verify_mcp_binary "$CONTAINER_NAME"

# ---------------------------------------------------------------------------
# Model profile
#
# opencode's `task` tool has no per-dispatch `model` parameter -- a subagent's
# model comes from its agent definition. So the tiering that
# `gova-build-execution` § Model Selection asks for has to be expressed as
# config, not as an argument at dispatch time. That is what this writes.
# ---------------------------------------------------------------------------

step "Choosing a model profile"

echo "  Which provider should this project's agents default to?"
echo ""
echo "    1) Anthropic     — opus-5 plans, sonnet-5 implements/reviews, haiku titles"
echo "    2) Ollama Cloud  — glm-5.3 plans/reviews, glm-5.3-flash implements, gpt-oss titles"
echo "    3) Leave unset   — everything inherits whatever model you pick in the TUI"
echo ""
printf "  Profile [1/2/3, default 3]: "
read -r PROFILE </dev/tty
PROFILE="${PROFILE:-3}"

MODEL_PROVIDER=""
case "$PROFILE" in
    1)
        MODEL_PROVIDER="anthropic"
        MODEL_LEAD="anthropic/claude-opus-5"
        MODEL_WORK="anthropic/claude-sonnet-5"
        MODEL_REVIEW="anthropic/claude-sonnet-5"
        MODEL_SMALL="anthropic/claude-haiku-4-5"
        ;;
    2)
        MODEL_PROVIDER="ollama-cloud"
        # glm-5.3 shipped ahead of the models.dev catalogue, so it resolves only
        # if it is declared under provider.ollama-cloud.models in
        # ~/.config/opencode/opencode.jsonc. The check below catches its absence.
        # glm-5.3-flash is already in the catalogue and needs no override.
        #
        # Flash implements, full reviews. The implementer works from a task brief
        # that already fixes the MCP call, the paths and the names -- the cheaper
        # model is authoring a customization against a spec, not deciding the
        # design. The reviewer is the only gate between that output and the next
        # task, so it does not get downgraded alongside the thing it checks.
        MODEL_LEAD="ollama-cloud/glm-5.3"
        MODEL_WORK="ollama-cloud/glm-5.3-flash"
        MODEL_REVIEW="ollama-cloud/glm-5.3"
        MODEL_SMALL="ollama-cloud/gpt-oss:20b"
        ;;
    3)
        ok "No model pinned — pick one with /models in the TUI"
        ;;
    *)
        fail "Unrecognised profile '$PROFILE' — choose 1, 2 or 3"
        ;;
esac

if [ -n "$MODEL_PROVIDER" ]; then
    ok "Profile: $MODEL_PROVIDER — plan $MODEL_LEAD, implement $MODEL_WORK, review $MODEL_REVIEW"
    # `opencode models <provider>` prints "Provider not found" when the provider
    # has no credentials, so this catches an unauthenticated profile here rather
    # than at the first /build.
    if opencode models "$MODEL_PROVIDER" 2>&1 | grep -q "Provider not found"; then
        warn "$MODEL_PROVIDER has no credentials yet — run: opencode auth login"
        if [ "$MODEL_PROVIDER" = "ollama-cloud" ]; then
            warn "  choose 'Ollama Cloud' and paste a key from https://ollama.com/settings/keys"
        fi
    else
        ok "$MODEL_PROVIDER credentials found"
        for m in "$MODEL_LEAD" "$MODEL_WORK" "$MODEL_REVIEW" "$MODEL_SMALL"; do
            opencode models "$MODEL_PROVIDER" 2>/dev/null | grep -qx "$m" \
                || warn "$m is not in this provider's model list — declare it under provider.$MODEL_PROVIDER.models in ~/.config/opencode/opencode.jsonc"
        done
    fi
fi

step "Generating opencode.json"

python3 - "$CONTAINER_NAME" "$SCRIPT_DIR" "${MODEL_LEAD:-}" "${MODEL_WORK:-}" "${MODEL_REVIEW:-}" "${MODEL_SMALL:-}" <<'PYEOF'
import json, sys, os

container, project_dir, lead, work, review, small = sys.argv[1:7]
config_path = os.path.join(project_dir, "opencode.json")

# This file is machine-specific and gitignored -- it is opencode's counterpart
# to .mcp.json. The committed, machine-independent half lives in
# .opencode/opencode.json; opencode deep-merges the two, project root first.
config = {
    "$schema": "https://opencode.ai/config.json",
    "mcp": {
        "gova-builder": {
            "type": "local",
            "command": ["docker", "exec", "-i", container, "/usr/local/bin/mcp-server"],
            "enabled": True,
            # A scaffold call renders templates and writes several files; the
            # 5s default is not enough for a cold container.
            "timeout": 120000,
        }
    },
}

if lead:
    config["model"] = lead
    config["small_model"] = small
    # Agent bodies live in .opencode/agent/*.md, which is loaded after this file
    # and carries no `model` key -- so these survive the merge.
    config["agent"] = {
        "gova-implementer": {"model": work},
        "gova-reviewer": {"model": review},
        "gova-architect": {"model": lead},
    }

with open(config_path, "w") as f:
    json.dump(config, f, indent=2)
    f.write("\n")

print(f"  + opencode.json → gova-builder via {container}")
if lead:
    print(f"  + plan {lead} | implement {work} | review {review} | small {small}")
PYEOF

ok "opencode.json generated"

step "Verifying opencode picks everything up"

python3 - <<'PYEOF'
import json, subprocess, sys

try:
    raw = subprocess.run(["opencode", "debug", "config"], capture_output=True, text=True, timeout=180).stdout
    cfg = json.loads(raw)
except Exception as e:  # noqa: BLE001 - any failure here is advisory, not fatal
    print(f"  ! could not read resolved config ({e}) — check manually with: opencode debug config")
    sys.exit(0)

commands = sorted((cfg.get("command") or {}).keys())
mcp = sorted((cfg.get("mcp") or {}).keys())
agents = sorted(a for a in (cfg.get("agent") or {}) if a.startswith("gova-"))
print(f"  commands: {', '.join(commands) or 'none'}")
print(f"  mcp:      {', '.join(mcp) or 'none'}")
print(f"  agents:   {', '.join(agents) or 'none'}")

missing = [c for c in ("build", "launch", "security-analyze") if c not in commands]
if missing:
    print(f"  ! missing commands: {', '.join(missing)} — check .opencode/command/ symlinks")
PYEOF

python3 - <<'PYEOF'
import json, subprocess, sys

try:
    raw = subprocess.run(["opencode", "debug", "skill"], capture_output=True, text=True, timeout=180).stdout
    skills = [s["name"] for s in json.loads(raw)]
except Exception as e:  # noqa: BLE001
    print(f"  ! could not read skills ({e}) — check manually with: opencode debug skill")
    sys.exit(0)

found = [s for s in skills if s.startswith("gova-")]
print(f"  skills:   {', '.join(sorted(found)) or 'none'}")
if len(found) < 3:
    print("  ! expected gova-brainstorm, gova-writing-plans, gova-build-execution")
    print("    opencode reads .claude/skills/ directly — check that directory exists")
PYEOF

ok "opencode configuration verified"

echo ""
echo "======================================"
echo -e "${GREEN}${BOLD}Setup complete!${NC}"
echo ""
echo "  1. Fill in SEED.md with your app idea"
echo "  2. Add API keys to .env if needed"
echo "  3. Open opencode:     opencode"
echo "  4. Verify MCP tools:  opencode mcp list  (gova-builder, context7, stripe)"
echo "  5. Start building:    /build"
echo ""
echo "  Commands: /build, /launch, /security-analyze"
echo "  Context:  AGENTS.md (a symlink to CLAUDE.md — one file, both harnesses)"
echo ""
