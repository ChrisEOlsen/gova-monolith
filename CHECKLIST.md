# Setup Checklist

## First-Time Setup
- [ ] Clone this repo
- [ ] Run `./install-claude.sh` (Claude Code) and/or `./install-opencode.sh` (opencode)
- [ ] Open your AI tool in this directory
- [ ] Verify MCP tools are connected — `/mcp` in Claude Code, `opencode mcp list` in opencode → should show `gova-builder` tools

## Before `/build`
- [ ] `SEED.md` filled in with app name, features, auth requirements
- [ ] `.env` has all required API keys for integrations checked in SEED.md

## Before `/launch`
- [ ] App reviewed and working at `http://localhost:[APP_PORT]`
- [ ] `TUNNEL_TOKEN` set in `.env`
- [ ] Domain configured in Cloudflare dashboard (Zero Trust → Tunnels)
