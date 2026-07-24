# GOVA Monolith: AI-Second

A template repository for building AI-driven web applications with the GOVA stack.

**G**o · **V**anilla JS · **A**lpine-free · SQLite WAL

## Core Idea

The AI doesn't write the important code — it calls MCP tools that render deterministic templates. No HTMX, no Alpine.js, no Templ compile step. Go handles JSON API. Vanilla ES modules handle all DOM rendering.

**Two containers, one SQLite file.** `app` runs the Go server; `mcp` runs the builder tools so restarting `app` never disconnects Claude Code. No Redis, no MySQL, no Nginx, no frontend build step.

## Built In, Not Bolted On

- **Auth, done right by default.** Signed HMAC-SHA256 sessions, double-submit CSRF, bcrypt with timing-safe comparison, rate-limited login (5 attempts / 15 min). Scaffold it once with `scaffold_auth`; the security work is already in the template.
- **Machine-readable API contract.** Every scaffold self-registers into `src/app/api.json` — a manifest of models (with types and nullability) and endpoints, served at `GET /api/v1/_manifest`. Routes are generated from it (`handlers/routes_gen.go`), so `main.go` is never hand-wired. `scaffold_resource` generates full CRUD (list/detail/create/update/delete + `?sort=`/`?filter=`); native clients (see [gova-ios](../gova-ios)) read the manifest instead of reverse-engineering source.
- **Scaffolds ship with tests.** Every model, handler, and auth endpoint the MCP tools generate comes with a Go test alongside it — CRUD roundtrips, login/CSRF/rate-limit coverage, mobile bearer-token flows. `docker compose exec app go test ./...` runs all of it.
- **Design intelligence on tap.** The `ui-ux-pro-max` skill brings searchable color palettes, typography pairings, and UX guidelines into the build — no separate design pass required.

## Quick Start

```bash
cp env.example .env
# Edit .env: set APP_NAME, SESSION_SECRET (openssl rand -hex 32)
```

| Tool | Install | Context file | Commands |
|---|---|---|---|
| **Claude Code** | `./install-claude.sh` | `CLAUDE.md` | `/build`, `/launch` |

Then:
1. Fill in `SEED.md` with your app idea
2. Run `/build`
3. Review the running app at `http://localhost:[APP_PORT]`
4. Run `/launch` to go live via Cloudflare Tunnel

## Stack

| Layer | Technology |
|---|---|
| Language | Go 1.25 |
| Router | chi |
| Frontend | Vanilla ES modules (no bundler) |
| Database | SQLite (WAL mode) |
| CSS | Tailwind CLI |
| Sessions | Signed cookies (HMAC-SHA256) |
| Cache | In-process map + mutex |
| Deployment | Cloudflare Tunnel |

## Token Efficiency

Each feature costs ~1,000 tokens to scaffold — the MCP server renders templates, not the LLM.
