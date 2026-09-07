# Agent Context: GOVA Monolith

You are the Lead Architect of a GOVA Monolith: Go + chi, SQLite, vanilla ES
modules, Tailwind. Run `/build` to build from `SEED.md`, `/launch` to deploy.

> Claude Code reads this as `CLAUDE.md`; opencode reads `AGENTS.md`, a symlink
> to it. One copy, no drift. See **Harnesses** at the end.

## How this works

**You do not write this application by hand. You drive a generator.** The
`./gova` CLI renders deterministic Go and JS from templates. Decide *what* to
build, run the right command, then customize what it produced. Generated code
arrives already wired, already tested, and already obeying the rules below.

**Two containers, one database.** `app` runs the Go server — restart it to
rebuild the binary and recompile CSS. `builder` holds the `gova` binary and its
templates, kept separate so `docker compose restart app` cannot kill a scaffold
mid-write. SQLite lives at `/data/app.db`. `./gova` is a thin wrapper that
execs into the builder container; run it from anywhere in the repo.

> The `builder` image embeds its templates at **image build** time. After
> editing anything under `src/builder/`, run `docker compose up -d --build
> builder` — a plain restart reruns the old binary and silently generates
> old-shape code.

**`src/app/api.json` is the source of truth for the served surface.** Models,
endpoints and pages live there. The tools write it and regenerate
`handlers/routes_gen.go` and `handlers/pages_gen.go` from it; `main.go` mounts
both with one call each. **Never hand-wire a route and never edit a `*_gen.go`
file** — if a route is wrong, the manifest is wrong.

**Auth ships with the template, and everything you generate is behind it.**
Sessions, CSRF, bcrypt, rate limiting, and bearer tokens for native clients are
committed code in `src/app`, not something you scaffold. Every route and page a
`gova` command emits requires a signed-in caller; `-public` is how you open one,
and you should have a reason. See `docs/API-CONTRACT.md`.

**Where things go.** Go handlers return JSON only, in `handlers/`. Database
access is model methods only, in `models/`. Page shells are inert HTML in
`static/pages/`. All DOM rendering is vanilla ES modules in `static/js/`.

**The loop for any feature:** create the table → call a scaffold tool →
customize what it generated → restart. Start with `./gova inspect`.

**Before claiming anything works:** `docker compose exec app go test ./...` and
`docker compose logs app`. Both, not either.

---

## The Golden Recipe

### 1. Table first
Always `id INTEGER PRIMARY KEY` and `created_at DATETIME DEFAULT
CURRENT_TIMESTAMP` — both are required and checked at scaffold time.

**If the rows belong to a user, add the owner column and scaffold with
`-owner`.** Ask this question for every table: would one user seeing another's
rows be a bug? If yes, it is an owned resource.

```sql
CREATE TABLE projects (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    status     TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### 2. Scaffold

```bash
./gova sql -query "CREATE TABLE projects (...)"
./gova resource -name project -fields name:string,status:string -owner
```

That one command writes the model, five CRUD handlers, and a page with a create
form and delete buttons, and registers all of it — behind `RequireAuth`, and
with every query scoped to the session user. For anything that is not a CRUD
resource, use `./gova model`, `./gova page` and `./gova handler`.

`user_id` is never in `-fields`: it is an implicit column like `id` and
`created_at`, set from the session and absent from the request body, the
response JSON and the manifest. Declaring it is an error.

### 3. Customize
Edit the generated `.js` for behavior and `.html` for layout. Go logic stays in
`handlers/`, data access in `models/`.

### 4. Restart
`docker compose restart app` rebuilds the binary and recompiles Tailwind.

---

## Mandatory Scaffolding Rule

**For a feature handler or a JS page, run the `gova` command FIRST — before
writing any code.** The sequence is always: **command → generated file →
customize it.**

Never write a feature handler from scratch and then run the command. Never skip
`./gova resource` because "it's simpler to just write it".

**Infrastructure is hand-written** — `middleware/`, `db/`, `cache/`,
`handlers/json.go`, `handlers/auth*.go`, `models/user.go`, `static/js/lib/*.js`.
These are app-wide plumbing created once, not per-feature. If you hand-write
something, say which rule made it infrastructure.

---

## Critical Constraints

1. **No raw SQL in handlers.** Model methods only — `model.GetPage(...)`, never
   `db.Query(...)`.
2. **No HTML rendering in Go.** Handlers return JSON. (Serving an inert `.html`
   shell via `http.ServeFile` is not rendering.)
3. **JS safety:**
   - NEVER `element.innerHTML = userValue` — use `textContent`, or
     `createElement` + `setAttribute` for structure.
   - NEVER `eval()` or `new Function()` with external data.
   - ALWAYS fetch through `api.js` — never a raw `fetch()`.
   - NEVER `console.log()` a token, password, or session value.
4. **No Node or npm.** Tailwind standalone only. No CDN script tags — the CSP
   blocks them anyway.
5. **Security is already wired, and on by default.** CSRF, sessions, rate
   limiting, bcrypt and the CSP live in `middleware/` and `handlers/`.
   Everything scaffolded is registered `auth: true`; open a route with
   `-public`. Do not re-check auth inside a handler — the route wrap does it.
   Reading `middleware.UserID(r)` to scope a query is not a re-check.
6. **A hand edit to `api.json` needs `./gova regen`.** The `*_gen.go` files are
   rendered from the manifest, not read from it at runtime, so flipping `auth`
   by hand changes nothing until you regenerate. `./gova inspect` reports the
   mismatch.

Full wire details — envelope, codes, timestamps, pagination, the manifest
fields native clients read — are in **`docs/API-CONTRACT.md`**. Read it before
changing anything a client can see. **`docs/DECISIONS.md`** explains why the
non-obvious pieces are shaped the way they are.

---

## Commands

Run `./gova help`, or `./gova <command> -h` for a command's flags.

| Command | Use | Tests? |
|---|---|---|
| `./gova inspect` | **First.** Manifest, files on disk, divergence | — |
| `./gova sql -query "..."` | Create tables — always before a model | — |
| `./gova model -name x -fields ...` | Data layer only; registers the model, no route | Yes |
| `./gova handler -name x -method POST -path /api/v1/...` | One custom JSON endpoint; self-registers | No — write one |
| `./gova page -file x -title X -path /x` | `.html` + `.js` at a human URL; no Go handler needed | Yes |
| `./gova resource -name x -fields ...` | Full CRUD + page + form. Table must exist | Yes |
| `./gova regen` | Re-render the `*_gen.go` files after editing `api.json` by hand | — |

**Access control flags.** `-public` (on `page`, `handler`, `resource`) opens a
route to anonymous callers; without it everything is guarded. `-owner` (on
`model`, `resource`) scopes every generated query to the session user — the
table must carry `user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE
CASCADE`, and another user's row answers 404, never 403. `-owner` and `-public`
are refused together.

`gova page` refuses `/api/`; `gova handler` requires `/api/v1/`. The two
namespaces cannot collide. Resource pages are plural (`/projects`); the auth
pages are singular (`/login`, `/register`).

**Fields** are a comma-separated `name:type` list —
`-fields "title:string,quantity:int,due_at:datetime"`. Field names must be
alphanumeric and underscore only: they are interpolated into generated Go, JS
and HTML.

**Types:** `string`, `int`, `float`, `boolean`, `timestamp`. An unknown type is
an error. A DATETIME column must be `timestamp`, never `string`.
`name:ref:<model>` is a foreign key; `name:email|date|datetime|time|json` is a
string with a format hint that drives the form control on web and iOS.

**Credential columns are refused.** The tools generate generic CRUD, where a
request that omits a field overwrites it. Authentication is hand-written.

---

## Testing

- Scaffold tools generate tests alongside the code they emit.
- **Hand-customized logic gets its own test.** Same `_test.go` convention,
  `httptest` against the handler, `db.OpenTest(t, extraSchema)` for anything
  touching the database (temp file, never `/data/app.db`).
- `pages_gen_test.go` is regenerated with `pages_gen.go` and asserts every
  registered page serves its shell. Never hand-edit it.
- **No JS test runner** — Constraint 4 rules out Node. Client code is verified
  in the browser.
- Verify with `docker compose exec app go test ./...` **and**
  `docker compose logs app`.

To check that a template change still produces code that *compiles*, not merely
parses:

```bash
docker run --rm -v "$PWD":/w -w /w golang:1.25 sh -c '
  rm -rf /scratch && cp -r /w/src/app /scratch
  cd /w/src/builder && SCRATCH_APP_DIR=/scratch go test -run TestRenderResourceToDir -count=1 .
  cd /scratch && go build ./... && go test ./handlers/ ./models/'
```

---

## How a build runs

**Subagents author code; the orchestrator touches everything shared.** Up to 3
implementers customize their own scaffold output at once, and none of them runs
`gova`, restarts the app, runs the suite, or commits. Those are the orchestrator's,
one batch at a time.

That split is what makes parallelism safe without any locking. Two tasks with
disjoint file lists still share the `app` container, the Go package that
`go test ./...` compiles, and the git index — so nobody but the orchestrator
touches them. See `gova-build-execution` § The model.

**The builder serializes itself regardless.** Every mutating `gova` command holds
an flock on `/src/.gova-lock` across its whole read→write→regenerate transaction
(`src/builder/lock.go`), and every file lands via temp-file + rename. That is
what keeps `api.json` correct when two harness sessions are open at once.
Verified: four concurrent `gova resource` processes register all four resources
with nothing lost.

**No worktrees.** The `builder` container's bind mounts point at one absolute
path, fixed at `docker compose up`. A worktree lives elsewhere, so `./gova`
would write to the wrong checkout. Use a plain feature branch in the main
checkout (`git checkout -b build/<app-name>`).

---

## Harnesses

This project runs under **Claude Code** and **opencode**, with the same
workflow, because everything defining it lives in files both read:

| What | Where | How each finds it |
|---|---|---|
| These rules | `CLAUDE.md` | Claude Code directly; opencode via the `AGENTS.md` symlink |
| `/build`, `/launch`, security audit | `.claude/commands/` | Claude Code reads the dir; `.opencode/command/*.md` symlink into it |
| The three `gova-*` skills | `.claude/skills/` | Both — opencode scans `.claude/skills/**/SKILL.md` |
| The `gova` builder | `./gova` in the repo root | Both — it is a CLI, not an MCP server |

Install with `./install-claude.sh`, `./install-opencode.sh`, or both.
**Nothing above is duplicated per harness. Do not fork it.**

Four differences:

1. **Batched questions** — `AskUserQuestion` (Claude Code) / `question`
   (opencode). Both take several questions per call.
2. **Subagent model** — Claude Code takes `model` per dispatch; opencode's
   `task` tool does not, so the tiers live in `.opencode/agent/*.md`. Under
   opencode, dispatch `gova-implementer`, `gova-reviewer`, or `gova-architect`.
3. **Final review** — the `code-review` skill (Claude Code), or `/review` /
   `gova-architect` (opencode).
4. **Command names** — the security audit is `/security:analyze` in Claude Code
   and `/security-analyze` in opencode.

opencode loads its config once at startup. After editing `opencode.json`,
anything under `.opencode/`, a skill, or a command, quit and reopen it.
