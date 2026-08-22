# MCP Server Container Split Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the MCP server out of the `app` container into its own `mcp` container so `docker compose restart app` no longer kills Claude Code's MCP connection.

**Architecture:** One multi-stage `Dockerfile` produces an `app` target (unchanged — live-builds the Go app on every start) and a new `mcp` target (`gcr.io/distroless/base-debian12` + the precompiled `mcp-server` binary, nothing else). Both containers bind-mount the same `./src` and `./data` host directories, so the MCP server keeps working on the same files without needing the app process alive. `build_css` and `run_linter` tools are dropped — they were the only two tools that shell out (`tailwindcss`, `go vet`), and dropping them means the `mcp` container needs no Go toolchain or tailwindcss binary at runtime.

**Tech Stack:** Go 1.25 (cgo + `mattn/go-sqlite3`), `mark3labs/mcp-go`, Docker multi-stage builds, `gcr.io/distroless/base-debian12`.

## Global Constraints

- No `exec.Command`/`os/exec` usage may remain anywhere in `src/builder/main.go` after this change — that's what makes the distroless runtime image valid (per spec: "MCP server container split").
- New MCP container name pattern: `${APP_NAME}-mcp-1` (was `${APP_NAME}-app-1`).
- The `mcp` service has no `depends_on: app` and publishes no ports — its lifecycle must be fully independent of `app`.
- `CLAUDE.md`, `AGENTS.md`, `GEMINI.md` are byte-identical in this repo (verified via `diff`) — any edit to one must be applied identically to all three.
- `runPatternChecks()` in `main.go` is used by 7 other handlers (not just the linter) — it must NOT be deleted, only `handleRunLinter` and `handleBuildCSS` are removed. (Corrects an inaccuracy in the design spec, which said to delete `runPatternChecks` too — it doesn't shell out and has no reason to go.)

---

### Task 1: Trim `build_css` / `run_linter` from the MCP server

**Files:**
- Modify: `src/builder/main.go`

**Interfaces:**
- Produces: `mcp-server` binary with exactly these 10 registered tools: `inspect_app`, `execute_sql`, `create_model`, `create_handler`, `create_page`, `scaffold_list`, `scaffold_auth`, `scaffold_registration`, `add_js_form`, `scaffold_mobile_auth`. `build_css` and `run_linter` no longer exist.

- [ ] **Step 1: Remove the now-unused `fmt` and `os/exec` imports**

In `src/builder/main.go`, replace the import block:

```go
import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	_ "github.com/mattn/go-sqlite3"
)
```

with:

```go
import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	_ "github.com/mattn/go-sqlite3"
)
```

- [ ] **Step 2: Drop the `build_css` reference from `scaffold_list`'s description**

Replace:

```go
	s.AddTool(mcp.NewTool("scaffold_list",
		mcp.WithDescription("Generate 4 files: model + JSON list handler + HTML shell + JS module. After: add forms with add_js_form, wire route in main.go, call build_css."),
```

with:

```go
	s.AddTool(mcp.NewTool("scaffold_list",
		mcp.WithDescription("Generate 4 files: model + JSON list handler + HTML shell + JS module. After: add forms with add_js_form, wire route in main.go."),
```

- [ ] **Step 3: Remove the `build_css` tool registration**

Replace:

```go
	s.AddTool(mcp.NewTool("build_css",
		mcp.WithDescription("Compile Tailwind CSS: static/css/input.css → static/css/style.css. Call after editing HTML classes."),
		mcp.WithBoolean("minify", mcp.Description("Minify output")),
	), handleBuildCSS)

	s.AddTool(mcp.NewTool("scaffold_mobile_auth",
```

with:

```go
	s.AddTool(mcp.NewTool("scaffold_mobile_auth",
```

- [ ] **Step 4: Remove the `run_linter` tool registration**

Replace:

```go
	s.AddTool(mcp.NewTool("run_linter",
		mcp.WithDescription("Run 'go vet ./...' and check handlers + JS files for raw SQL, innerHTML XSS patterns. Run after scaffolding to verify generated code."),
	), handleRunLinter)

	if err := server.ServeStdio(s); err != nil {
```

with:

```go
	if err := server.ServeStdio(s); err != nil {
```

- [ ] **Step 5: Drop the `build_css` mention from `handleScaffoldList`'s result text**

Replace:

```go
	return mcp.NewToolResultText(
		strings.Join(results, "\n") +
			"\n\nNext: wire GET route in main.go, add POST handler with create_handler, add form with add_js_form, call build_css.\n\n" +
			runPatternChecks(),
	), nil
}
```

with:

```go
	return mcp.NewToolResultText(
		strings.Join(results, "\n") +
			"\n\nNext: wire GET route in main.go, add POST handler with create_handler, add form with add_js_form.\n\n" +
			runPatternChecks(),
	), nil
}
```

- [ ] **Step 6: Delete `handleBuildCSS`**

Replace:

```go
func handleBuildCSS(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	minify, _ := req.Params.Arguments["minify"].(bool)
	args := []string{
		"-i", "/src/app/static/css/input.css",
		"-o", "/src/app/static/css/style.css",
		"--content", "/src/app/static/**/*.html,/src/app/static/**/*.js",
	}
	if minify {
		args = append(args, "--minify")
	}
	cmd := exec.CommandContext(ctx, "/usr/local/bin/tailwindcss", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errResult(fmt.Sprintf("tailwindcss failed:\n%s", string(out))), nil
	}
	return mcp.NewToolResultText("CSS compiled to /src/app/static/css/style.css"), nil
}
func handleScaffoldMobileAuth(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
```

with:

```go
func handleScaffoldMobileAuth(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
```

- [ ] **Step 7: Delete `handleRunLinter`**

Replace (this is the end of the file — delete through EOF):

```go

func handleRunLinter(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cmd := exec.CommandContext(ctx, "go", "vet", "./...")
	cmd.Dir = "/src/app"
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errResult(fmt.Sprintf("go vet failed:\n%s", string(out))), nil
	}

	bannedPatterns := []struct{ pattern, message string }{
		{`db\.Exec\(fmt\.Sprintf`, "SQL injection risk: use prepared statements"},
		{`db\.Query\(fmt\.Sprintf`, "SQL injection risk: use prepared statements"},
		{`\.innerHTML\s*=`, "XSS risk: use textContent or createElement instead of innerHTML"},
	}

	violations := []string{}
	goFiles, _ := filepath.Glob("/src/app/handlers/*.go")
	jsFiles, _ := filepath.Glob("/src/app/static/js/*.js")
	for _, file := range append(goFiles, jsFiles...) {
		content, _ := os.ReadFile(file)
		for _, bp := range bannedPatterns {
			re := regexp.MustCompile(bp.pattern)
			if re.Match(content) {
				violations = append(violations, fmt.Sprintf("%s: %s", filepath.Base(file), bp.message))
			}
		}
	}

	if len(violations) > 0 {
		return errResult("Linter found issues:\n" + strings.Join(violations, "\n")), nil
	}
	return mcp.NewToolResultText("go vet + pattern checks passed"), nil
}
```

with nothing (delete the whole block, leave the file ending after `mobileAuthRouteInstructions`'s closing brace).

- [ ] **Step 8: Build and smoke-test locally**

Run:

```bash
cd src/builder && CGO_ENABLED=1 go build -o /tmp/mcp-server-test . && cd -
```

Expected: exits 0, no compile errors (confirms the deleted functions' only callers were their own tool registrations, and `runPatternChecks`/`os`/`filepath`/`regexp`/`strings` are all still used elsewhere).

Then confirm the tool list is exactly the 10 expected tools:

```bash
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.0.1"}}}\n{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}\n' \
  | /tmp/mcp-server-test 2>&1 | tail -1 | grep -o '"name":"[a-z_]*"' | sort -u
```

Expected output (exactly these 10 lines, no `build_css` or `run_linter`):

```
"name":"add_js_form"
"name":"create_handler"
"name":"create_model"
"name":"create_page"
"name":"execute_sql"
"name":"inspect_app"
"name":"scaffold_auth"
"name":"scaffold_list"
"name":"scaffold_mobile_auth"
"name":"scaffold_registration"
```

- [ ] **Step 9: Commit**

```bash
git add src/builder/main.go
git commit -m "refactor: drop build_css/run_linter MCP tools

Both only existed to shell out to tailwindcss/go vet. Removing them
means the MCP server has zero exec.Command calls, so it can run in a
minimal distroless container with no Go toolchain."
```

---

### Task 2: Multi-stage Dockerfile — `app` and `mcp` targets

**Files:**
- Modify: `Dockerfile`

**Interfaces:**
- Produces: build stage named `builder` (compiles `mcp-server`, installs `tailwindcss`, runs `go mod tidy` for `src/app`); final stage `app` (`docker build --target app`, unchanged runtime behavior — runs `entrypoint.sh`); final stage `mcp` (`docker build --target mcp`, `ENTRYPOINT ["/usr/local/bin/mcp-server"]`).

- [ ] **Step 1: Replace the entire Dockerfile**

Replace the full contents of `Dockerfile` with:

```dockerfile
FROM golang:1.25 AS builder

RUN apt-get update && apt-get install -y --no-install-recommends gcc curl git && rm -rf /var/lib/apt/lists/*

# Tailwind CSS standalone binary
RUN ARCH=$(uname -m) && \
    if [ "$ARCH" = "aarch64" ]; then TW_ARCH="linux-arm64"; else TW_ARCH="linux-x64"; fi && \
    curl -sL "https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-${TW_ARCH}" \
        -o /usr/local/bin/tailwindcss \
    && chmod +x /usr/local/bin/tailwindcss

# Build MCP server binary
WORKDIR /src/builder
COPY src/builder/ ./
RUN go mod tidy
RUN CGO_ENABLED=1 go build -o /usr/local/bin/mcp-server .

# Pre-download app dependencies
WORKDIR /src/app
COPY src/app/ ./
RUN go mod tidy

# ---- app: unchanged runtime, live-builds the Go app on every container start ----
FROM builder AS app
WORKDIR /src/app
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
EXPOSE 8080
CMD ["/entrypoint.sh"]

# ---- mcp: nothing but the compiled binary + glibc for the cgo sqlite3 driver ----
FROM gcr.io/distroless/base-debian12 AS mcp
COPY --from=builder /usr/local/bin/mcp-server /usr/local/bin/mcp-server
ENTRYPOINT ["/usr/local/bin/mcp-server"]
```

- [ ] **Step 2: Verify both targets build**

Run:

```bash
docker build --target app -t gova-app-test . && docker build --target mcp -t gova-mcp-test .
```

Expected: both build to completion with exit 0.

- [ ] **Step 3: Verify the mcp image has no Go toolchain and runs the binary**

Run:

```bash
docker run --rm --entrypoint sh gova-mcp-test -c 'which go' ; echo "exit: $?"
```

Expected: `exit: 1` (no shell either — this actually fails to even find `sh` since distroless has none; that failure itself confirms there's no Go toolchain or shell in the image). Then confirm the binary alone starts and waits on stdio:

```bash
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.0.1"}}}\n{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}\n' \
  | docker run --rm -i gova-mcp-test | tail -1 | grep -c '"name":"inspect_app"'
```

Expected: `1`.

- [ ] **Step 4: Commit**

```bash
git add Dockerfile
git commit -m "build: split Dockerfile into app and mcp targets

mcp target is a minimal distroless image with just the compiled
mcp-server binary — no Go toolchain, no tailwindcss, no shell."
```

---

### Task 3: `docker-compose.yml` — add the `mcp` service

**Files:**
- Modify: `docker-compose.yml`

**Interfaces:**
- Consumes: `app`/`mcp` build targets from Task 2's `Dockerfile`.
- Produces: `mcp` service, container name `${APP_NAME}-mcp-1` under Compose's default naming, no ports published, `restart: unless-stopped`, no `depends_on`.

- [ ] **Step 1: Replace the entire docker-compose.yml**

Replace the full contents of `docker-compose.yml` with:

```yaml
name: ${APP_NAME:-my-gova-app}

services:
  app:
    build:
      context: .
      target: app
    ports:
      - "${APP_PORT:-8080}:8080"
    volumes:
      - ./src:/src
      - ./data:/data
      - ./logs:/logs
    env_file: .env

  mcp:
    build:
      context: .
      target: mcp
    volumes:
      - ./src:/src
      - ./data:/data
    restart: unless-stopped
```

- [ ] **Step 2: Verify the compose file is valid**

Run:

```bash
docker compose config --quiet
```

Expected: exits 0, no output (a non-zero exit or YAML error means the file is malformed).

- [ ] **Step 3: Commit**

```bash
git add docker-compose.yml
git commit -m "build: add mcp service to docker-compose.yml

Independent lifecycle from app — no depends_on, no ports. Bind-mounts
the same ./src and ./data as app so the MCP server keeps working on
the same files without needing the app process alive."
```

---

### Task 4: `install-claude.sh` — point `.mcp.json` at the `mcp` container

**Files:**
- Modify: `install-claude.sh:74-75`

**Interfaces:**
- Produces: `.mcp.json` whose `docker exec` target is `${APP_NAME}-mcp-1` instead of `${APP_NAME}-app-1`.

- [ ] **Step 1: Update `CONTAINER_NAME` to point at the mcp container**

Replace:

```bash
CONTAINER_NAME="${APP_NAME}-app-1"
ok "Container: $CONTAINER_NAME"
```

with:

```bash
CONTAINER_NAME="${APP_NAME}-mcp-1"
ok "MCP container: $CONTAINER_NAME"
```

(No other lines change — `CONTAINER_NAME` is reused as-is by the binary-presence check at line 142 and the `.mcp.json` generator at line 150.)

- [ ] **Step 2: Verify the script still parses**

Run:

```bash
bash -n install-claude.sh
```

Expected: exits 0, no syntax errors.

- [ ] **Step 3: Commit**

```bash
git add install-claude.sh
git commit -m "fix: point install-claude.sh at the mcp container, not app

.mcp.json must docker exec into the mcp container now that the MCP
server lives there instead of inside app."
```

---

### Task 5: Update workflow docs to drop `build_css` / `run_linter`

**Files:**
- Modify: `CLAUDE.md`, `AGENTS.md`, `GEMINI.md` (byte-identical — apply the same 3 edits to each)
- Modify: `.claude/commands/build.md`, `.opencode/commands/build.md` (byte-identical — apply the same 3 edits to each)
- Modify: `.gemini/commands/build.toml`

**Interfaces:** none (documentation only).

- [ ] **Step 1: Edit `CLAUDE.md`, `AGENTS.md`, `GEMINI.md` — replace the "Compile CSS" section**

In each of the three files, replace:

```markdown
### 4. Compile CSS
- ALWAYS call `build_css()` after adding or changing HTML classes.
```

with:

```markdown
### 4. CSS Compiles Automatically
- `entrypoint.sh` recompiles Tailwind CSS on every `docker compose restart app` — no MCP step needed.
- If you changed JS/HTML classes without any Go handler change, restart once to see them: `docker compose restart app`.
```

- [ ] **Step 2: Edit `CLAUDE.md`, `AGENTS.md`, `GEMINI.md` — drop the two Tool Cheat Sheet rows**

In each of the three files, replace:

```markdown
| `add_js_form` | Inject creation form into existing `.js` module |
| `build_css` | After editing HTML classes — compiles Tailwind |
| `run_linter` | `go vet` + SQL injection + innerHTML XSS checks |
```

with:

```markdown
| `add_js_form` | Inject creation form into existing `.js` module |
```

- [ ] **Step 3: Edit `CLAUDE.md`, `AGENTS.md`, `GEMINI.md` — trim the Custom / Escape Hatch Pattern list**

In each of the three files, replace:

```
1. execute_sql       → create the table
2. create_model      → generate the model
3. create_page       → html shell + js module + handler stub
4. create_handler    → POST/DELETE handler stubs as needed
5. edit handlers/    → implement TODO logic using model methods
6. edit static/js/   → fetch data, render DOM (never innerHTML for user data)
7. add_js_form       → inject form at // @inject-forms marker
8. build_css         → compile
9. run_linter        → verify
```

with:

```
1. execute_sql       → create the table
2. create_model      → generate the model
3. create_page       → html shell + js module + handler stub
4. create_handler    → POST/DELETE handler stubs as needed
5. edit handlers/    → implement TODO logic using model methods
6. edit static/js/   → fetch data, render DOM (never innerHTML for user data)
7. add_js_form       → inject form at // @inject-forms marker
8. docker compose restart app → recompiles CSS, rebuilds the Go binary
```

- [ ] **Step 4: Verify the three files are still byte-identical**

Run:

```bash
diff CLAUDE.md AGENTS.md && diff CLAUDE.md GEMINI.md
```

Expected: no output from either `diff` (exit 0 both times).

- [ ] **Step 5: Edit `.claude/commands/build.md` and `.opencode/commands/build.md`**

In each of the two files, apply these three replacements:

Replace:
```markdown
- One task per feature: `execute_sql` → `scaffold_*` → `add_js_form` → `build_css`
```
with:
```markdown
- One task per feature: `execute_sql` → `scaffold_*` → `add_js_form`
```

Replace:
```markdown
- Call `build_css()` after the final UI pass
```
with:
```markdown
- CSS recompiles automatically on `docker compose restart app` — restart once after a JS/HTML-only UI pass with no Go changes
```

Replace:
```markdown
- **Architecture:** Tables via `execute_sql`? Models via `create_model`? No raw SQL in handlers? JS never uses `innerHTML` with user data? `build_css()` called? Linter passed?
```
with:
```markdown
- **Architecture:** Tables via `execute_sql`? Models via `create_model`? No raw SQL in handlers? JS never uses `innerHTML` with user data?
```

- [ ] **Step 6: Verify the two build.md files are still byte-identical**

Run:

```bash
diff .claude/commands/build.md .opencode/commands/build.md
```

Expected: no output (exit 0).

- [ ] **Step 7: Edit `.gemini/commands/build.toml`**

Replace:
```
Constraints: Tasks are MCP tool calls. No TDD. execute_sql → scaffold_* → add_js_form → build_css.
```
with:
```
Constraints: Tasks are MCP tool calls. No TDD. execute_sql → scaffold_* → add_js_form.
```

Replace:
```
- call build_css() after final UI pass
```
with:
```
- CSS recompiles automatically on docker compose restart app — restart once after a JS/HTML-only UI pass with no Go changes
```

Replace:
```
Check: all features implemented, no innerHTML XSS, build_css called, linter passed, no hardcoded secrets.
```
with:
```
Check: all features implemented, no innerHTML XSS, no hardcoded secrets.
```

- [ ] **Step 8: Confirm no stale references remain in the live workflow files**

Run (targets only the files this task touches, not history docs like the spec/plan for this very change):

```bash
grep -n "build_css\|run_linter" \
  CLAUDE.md AGENTS.md GEMINI.md \
  .claude/commands/build.md .opencode/commands/build.md \
  .gemini/commands/build.toml \
  src/builder/main.go
```

Expected: exits 1 with no output (grep's "no match" exit code — confirms every reference in these specific files is gone).

- [ ] **Step 9: Commit**

```bash
git add CLAUDE.md AGENTS.md GEMINI.md .claude/commands/build.md .opencode/commands/build.md .gemini/commands/build.toml
git commit -m "docs: remove build_css/run_linter from workflow docs

Both tools were dropped from the MCP server (see main.go). CSS now
recompiles automatically on docker compose restart app instead of an
explicit build_css() call; there's no replacement for the linter."
```

---

### Task 6: Rebuild, regenerate `.mcp.json`, verify end-to-end

**Files:**
- Modify: `.mcp.json`

**Interfaces:** none — this task exercises everything built in Tasks 1–5 together.

- [ ] **Step 1: Point this repo's `.mcp.json` at the new mcp container**

Read the current `.env` to confirm `APP_NAME`:

```bash
grep '^APP_NAME=' .env
```

Expected: `APP_NAME=gove-test` (if different, substitute accordingly in the next step).

Replace the contents of `.mcp.json`:

```json
{
  "mcpServers": {
    "gova-builder": {
      "command": "docker",
      "args": [
        "exec",
        "-i",
        "gove-test-mcp-1",
        "/usr/local/bin/mcp-server"
      ]
    }
  }
}
```

- [ ] **Step 2: Rebuild and start both services**

Run:

```bash
docker compose up -d --build
```

Expected: both `app` and `mcp` services build and start with exit 0.

- [ ] **Step 3: Verify both containers are running**

Run:

```bash
docker compose ps --format '{{.Name}}: {{.Status}}'
```

Expected: two lines, `gove-test-app-1: Up ...` and `gove-test-mcp-1: Up ...`.

- [ ] **Step 4: Verify the mcp container serves tool calls over `docker exec`**

Run:

```bash
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.0.1"}}}\n{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}\n' \
  | docker exec -i gove-test-mcp-1 /usr/local/bin/mcp-server | tail -1 | grep -c '"name":"inspect_app"'
```

Expected: `1`.

- [ ] **Step 5: Verify `app` restarts do not touch `mcp`**

Run:

```bash
BEFORE=$(docker inspect -f '{{.State.StartedAt}}' gove-test-mcp-1)
docker compose restart app
AFTER=$(docker inspect -f '{{.State.StartedAt}}' gove-test-mcp-1)
[ "$BEFORE" = "$AFTER" ] && echo "mcp untouched" || echo "FAIL: mcp restarted"
```

Expected: `mcp untouched`.

- [ ] **Step 6: Verify a tool call still works after the app restart (the actual bug this plan fixes)**

Run:

```bash
printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.0.1"}}}\n{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"inspect_app","arguments":{}}}\n' \
  | docker exec -i gove-test-mcp-1 /usr/local/bin/mcp-server | tail -1 | grep -c '"Models:'
```

Expected: `1` (confirms `inspect_app` executed successfully post-restart, no reconnection needed).

- [ ] **Step 7: Commit**

```bash
git add .mcp.json
git commit -m "chore: point .mcp.json at the mcp container

Verified: app restarts no longer disconnect the MCP server."
```
