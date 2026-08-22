# MCP Server Container Split

## Problem

The MCP server (`src/builder/main.go`) is compiled into the `app` image and
invoked via `docker exec -i <app-container> /usr/local/bin/mcp-server`
(`.mcp.json`). Restarting the app container (`docker compose restart app` —
required after every Go handler change, per the Golden Recipe) kills the
`docker exec` session, disconnecting the MCP server from Claude Code mid-task.

## Root cause

The MCP server has no runtime dependency on the running app process. Its
tools only touch:
- `/src` (bind-mounted from host `./src`) — read/write Go, HTML, JS, template files
- `/data/app.db` (bind-mounted from host `./data`) — SQLite DDL/DML via `execute_sql`, `scaffold_auth`, `scaffold_mobile_auth`
- `tailwindcss` binary (`build_css`) and `go vet` (`run_linter`) — the only two tools that shell out

It was only ever colocated with `app` for convenience of sharing the image
and toolchain, not because it needs the app process alive.

## Decision: drop `build_css` and `run_linter`

Both tools exist solely to shell out to `tailwindcss` and `go vet`. Removing
them means the MCP server has **zero `exec.Command` calls** and needs no Go
toolchain or tailwindcss binary at runtime — only the compiled binary plus
glibc (for the cgo sqlite3 driver).

Consequence for the workflow: Tailwind CSS is not recompiled on demand
anymore. `entrypoint.sh` already runs `tailwindcss` on every `app` container
start, scanning the `@source` globs in `input.css`, so CSS catches up on the
next `docker compose restart app` — which happens anyway after any Go
handler change. The only gap is a pure JS/HTML-only edit with no Go change,
which now needs an explicit restart to see new Tailwind classes render.
`go vet` linting has no substitute — it's simply gone.

## Architecture

Two services, one multi-stage `Dockerfile`, both bind-mounting the same host
directories:

```
services:
  app:
    build: { context: ., target: app }
    ports: ["${APP_PORT:-8080}:8080"]
    volumes: [ ./src:/src, ./data:/data, ./logs:/logs ]
    env_file: .env

  mcp:
    build: { context: ., target: mcp }
    volumes: [ ./src:/src, ./data:/data ]
    restart: unless-stopped
```

No `depends_on` between them — fully independent lifecycles. `mcp` exposes
no ports (accessed only via `docker exec` from Claude Code's stdio MCP
transport).

### Dockerfile (multi-stage)

- **`builder` stage**: `golang:1.25` + `gcc`. Compiles `mcp-server`
  (`CGO_ENABLED=1`, same as today) and runs `go mod tidy` for `src/app`.
- **`app` stage**: `FROM golang:1.25`. Copies `src/app` + tailwindcss
  binary + `entrypoint.sh`. Unchanged from today — still live-builds the app
  binary on every container start via `entrypoint.sh`.
- **`mcp` stage**: `FROM gcr.io/distroless/base-debian12`. Copies only the
  compiled `mcp-server` binary from the `builder` stage. `ENTRYPOINT
  ["/usr/local/bin/mcp-server"]`.

### `main.go` changes

Delete `handleBuildCSS`, `handleRunLinter`, `runPatternChecks`, their tool
registrations in `main()`, and the now-unused `os/exec` import.

### `.mcp.json` / `install-claude.sh`

`CONTAINER_NAME` changes from `${APP_NAME}-app-1` to `${APP_NAME}-mcp-1`.
The binary-presence check (`install-claude.sh:139-146`) and the generated
`.mcp.json` (`install-claude.sh:150-171`) both point at the `mcp` container
instead of `app`.

### `CLAUDE.md` changes

- Tool Cheat Sheet: remove `build_css` and `run_linter` rows.
- Golden Recipe step 4 ("Compile CSS"): replace "ALWAYS call `build_css()`"
  with a note that CSS recompiles automatically on the next
  `docker compose restart app`, and that JS/HTML-only edits need an explicit
  restart to see new Tailwind classes.

## Data flow / concurrency

Both containers write to the same host-bind-mounted `/data/app.db`. SQLite
WAL mode handles cross-process file locking correctly as long as both
processes see the same underlying file — true here since both are bind
mounts of the same host path. macOS Docker Desktop (virtiofs) passes POSIX
locks through correctly, so no behavior change from today's single-process
access.

## Testing

1. `docker compose up -d --build` — both `app` and `mcp` come up healthy.
2. `docker exec <mcp-container> /usr/local/bin/mcp-server` responds to a
   tool call (e.g. `inspect_app`) over stdio.
3. `docker compose restart app` — `mcp` container shows no restart
   (`docker compose ps`), and a subsequent MCP tool call succeeds without
   Claude Code needing to reconnect.
4. Scaffold via MCP (`execute_sql` + `create_model`) — confirm generated
   files land in `./src/app` on the host and the `app` container picks them
   up after its own restart.

## Out of scope

- Restoring `build_css`/`run_linter` via a different mechanism (e.g. a
  lightweight sidecar or a `docker exec` into `app` specifically for these
  two tools) — rejected in favor of simplicity; revisit only if the lost
  linting/CSS-on-demand workflow proves painful in practice.
- Switching the sqlite driver to a pure-Go implementation (e.g.
  `modernc.org/sqlite`) to drop cgo entirely — not needed; distroless base
  already satisfies the glibc runtime dependency.
