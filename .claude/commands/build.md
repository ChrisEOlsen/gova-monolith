---
description: Build a new GOVA application from SEED.md — full automated workflow from spec to running app
---

You are running the GOVA build workflow. Read this file completely before taking any action.

---

## Step 1: Validate Context

Read `SEED.md`. If it is empty or contains only placeholder text, STOP and tell the developer to fill it in first.

Read `.env`. Verify `SESSION_SECRET` is set to something other than the placeholder. If it still says `change-me-to-32-random-bytes-before-use`, STOP and warn:

> "SESSION_SECRET in .env is still the placeholder value. Generate a secure secret before building:
> `openssl rand -hex 32`"

---

## Step 2: Brainstorm

Use the `gova-brainstorm` skill with the contents of `SEED.md` as input.

- Run its **Scale Gate** first and state the classification. A full `/build` from a fresh `SEED.md` is normally Standard or Large. An incremental change to an existing app is often Small — on that path there is no spec document, and Step 3's plan is the only written artifact.
- Batch clarifying questions with the harness's batched-question tool (`AskUserQuestion` in Claude Code, `question` in opencode) — features and data model in one call, auth/resources/integrations in the next. Do not ask one question per message.
- Wait for developer approval before proceeding

---

## Step 3: Write an Implementation Plan

Use the `gova-writing-plans` skill.

**Mandatory constraints for the plan:**
- Tasks are **MCP tool calls**, not Go code or JS written by hand
- The plan specifies contracts, not bodies — exact MCP tool calls, exact paths, and exact cross-task names and literals go in verbatim; customization bodies are described and the implementer writes them once. Do not pre-write the implementation into the plan.
- Scaffold-generated code (models, handlers, auth) already has tests from its scaffold call — only plan a test-writing step for hand-customized logic, per `gova-writing-plans` Step 3b
- One task per feature: `execute_sql` → `scaffold_*` → `add_js_form`
- Follow the Golden Recipe from `CLAUDE.md` for every feature
- For a resource that needs create/edit/delete, use `scaffold_resource` (full CRUD, self-registered) rather than `scaffold_list` (read-only). Reserve `scaffold_list` for reference/read-only data.
- Plan steps scaffold first, then customize. Never plan "implement X handler" — always start with the MCP scaffold tool.

---

## Step 4: Create Feature Branch

**Do not use `superpowers:using-git-worktrees` or any worktree.** The `gova-builder` MCP server and SQLite db are singleton, path-bound infrastructure (see `CLAUDE.md` § No Git Worktrees for Builds) — a worktree at a different path breaks the MCP container's bind mounts and forces a disruptive container remount + manual `/mcp` reconnect mid-build.

Create a plain feature branch directly in the current checkout instead: `git checkout -b build/<app-name>`.

Derive branch name from app name in SEED.md: "Task Manager" → `build/task-manager`

---

## Step 5: Implement

Use `gova-build-execution` to execute the plan.

### Mandatory Scaffolding Rule for every subagent:

**For feature handlers and JS pages, call the MCP tool FIRST — before writing any code.**

The sequence is always: **MCP tool → generated file → customize generated file**

NEVER (for feature files):
- Write a feature handler from scratch, then call MCP tools
- Skip `scaffold_list` because "it's simpler to just write it"
- Create a feature `.js` module without calling `create_page` or `scaffold_list` first

**Exception — infrastructure files are written manually** (created once at init, not per-feature):
- `middleware/*.go`, `db/`, `cache/` — app-wide plumbing and core infrastructure
- `handlers/json.go` — shared JSON helpers
- `static/js/lib/*.js` — shared libs (api.js, auth.js)
- Shared utility JS modules imported by other modules

Subagents must confirm at the start of each task:
> "Which MCP tool scaffolds this?" → call it → then customize.
> If it's infrastructure, document why no scaffold tool applies.

### Additional mandatory context for every subagent:
- Follow the Golden Recipe from CLAUDE.md
- Never write raw SQL in handler files — use model methods only
- CSS recompiles automatically on `docker compose restart app` — restart once after a JS/HTML-only UI pass with no Go changes
- **Design bar — build it slick.** Every page should look like a professionally designed product, not a scaffold with default styling. Concretely:
  - **Palette:** one accent color used deliberately, a neutral scale for everything else. Not five competing colors, not raw Tailwind defaults everywhere.
  - **Type:** real hierarchy through weight and size together, not size alone. Tight line-height on headings, comfortable on body.
  - **Spacing:** one consistent scale, applied generously. Cramped layouts are the single clearest tell of a scaffolded UI.
  - **Motion:** smooth and purposeful. Transitions on hover, focus, and state changes; entrance animation on lists, modals, and toasts; nothing that jitters, blocks input, or animates on every render. Wrap it in `@media (prefers-reduced-motion: reduce)` so it can be turned off.
  - **Interaction states:** visible hover, focus, active, and disabled states on everything interactive. Focus rings stay — style them, don't remove them.
  - **Empty, loading, and error states are designed, not blank.** A list with no rows shows something intentional.
  - Tailwind utilities plus CSS transitions cover all of this. No framework, no CDN, no JS animation library (Critical Constraint 4).
- Use the `context7` MCP server for external API documentation. Both installers
  register it alongside `stripe`. If it is not connected (`/mcp` in Claude Code,
  `opencode mcp list` in opencode), fall back to web search/fetch rather than
  stopping.
- Do not add manual cache calls to model methods — caching is automatic
- JS safety: NEVER use `element.innerHTML = userValue` (XSS). ALWAYS use `element.textContent` for user-supplied text. ALWAYS use `createElement` for structured HTML.

---

## Step 5b: Stripe Webhook Registration (if SEED.md has Payments checked)

If `[x] Payments (Stripe)` is in SEED.md:

> **The webhook lives at `/api/v1/stripe_webhook`, like every other endpoint.**
> This step used to say `/api/stripe_webhook`, which no tool in the workflow can
> build: `create_handler` refuses any path outside `/api/v1/`, and hand-wiring
> the route in `main.go` is forbidden. So a SEED.md with Payments checked had no
> legal way to create its own handler. Stripe does not care what the path is.
>
> Create the handler with `create_handler` during Step 5 like any other endpoint.
> It needs no CSRF exemption: the request arrives with neither a `csrf_token` nor
> a session cookie, which is the no-ambient-credential case `middleware.CSRF`
> already lets through.

1. Read `APP_URL` from `.env`. If empty, STOP:
   > "APP_URL is not set. Set it to your production domain before registering the Stripe webhook."
2. Register webhook via Stripe MCP: endpoint `${APP_URL}/api/v1/stripe_webhook`
3. Start local listener: `stripe listen --forward-to http://localhost:[APP_PORT]/api/v1/stripe_webhook`
4. Extract local webhook secret → write to `.env` as `STRIPE_WEBHOOK_SECRET`
5. Fire test event: `stripe trigger payment_intent.succeeded`
6. Verify handler returns 200 in `docker compose logs app`
7. Stop listener, restore production secret to `.env`

---

## Step 6: Security Analysis

Run the security audit command on `src/app/` — `/security:analyze` in Claude Code, `/security-analyze` in opencode.

---

## Step 7: Security Fixes (if needed)

If Critical, High, or Medium findings exist:
1. Write a targeted fix plan
2. Execute fixes
3. Re-run the security audit command

---

## Step 8: Pre-Completion Verification

**No completion claim without fresh verification evidence.** If you haven't run the check in this message, you cannot claim it passes. "Should work," "looks right," or trusting a subagent's self-report without checking the diff are not verification — run the actual check, read the actual output, then claim the result.

Verify, with evidence for each:
- **Features:** All SEED.md features implemented? Auth-required pages call `requireAuth()`? No placeholder text? (Read the files — don't infer from the plan.)
- **CRUD:** If a create form exists, do edit and delete exist?
- **Architecture:** Tables via `execute_sql`? Models via `create_model`? No raw SQL in handlers? JS never uses `innerHTML` with user data? (Grep for `innerHTML` and `db.Query`/`db.Exec` outside models/ to confirm, don't assume the rule held.)
- **Tests:** Run `docker compose exec app go test ./...` now and read the output — all passing? A failing test blocks completion the same as a failing build.
- **Design:** Does it meet the Step 5 design bar — deliberate palette, real type hierarchy, consistent spacing, smooth transitions on interactive elements, designed empty/loading states? Titles set? Mobile-responsive? (Load the pages and look; don't infer from the class names.)
- **App:** Run `docker compose logs app` now and read the output — no errors?
- **Environment:** New env vars documented in `env.example`? No hardcoded secrets?

If any check fails, fix it and re-run that specific check before moving on — don't batch fixes and assume they worked.

---

## Step 9: Merge Decision

Ask the developer:

> "Build complete on branch `build/[app-name]`. Merge to `main` now, or leave the branch as-is for you to review/PR first?"

**If merge:** `git checkout main && git merge build/[app-name] --no-edit && git branch -d build/[app-name]` (local merge only — do not push unless the developer explicitly asks).

**If leave as-is:** Report the branch name and stop. Do not merge or delete the branch.

---

## Step 10: Done

Report to the developer:

> **Build complete.**
>
> App running at: `http://localhost:[APP_PORT]`
> Branch: `build/[app-name]` [merged to main | left for review]
> Security report: `.security/SECURITY_REPORT.md`
>
> Next steps: review the running app, then run `/launch` to go live.
