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

Use the `superpowers:brainstorming` skill with the contents of `SEED.md` as input.

- Clarify the app's features and data model
- Confirm auth requirements, resource types, external integrations
- Wait for developer approval before proceeding

---

## Step 3: Write an Implementation Plan

Use the `superpowers:writing-plans` skill.

**Mandatory constraints for the plan:**
- Tasks are **MCP tool calls**, not Go code or JS written by hand
- Skip TDD — there is no test suite
- One task per feature: `execute_sql` → `scaffold_*` → `add_js_form`
- Follow the Golden Recipe from `CLAUDE.md` for every feature
- If a create form exists, plan edit + delete too (CRUD completeness)
- Plan steps scaffold first, then customize. Never plan "implement X handler" — always start with the MCP scaffold tool.

---

## Step 4: Create Feature Branch

Use `superpowers:using-git-worktrees` to create an isolated branch.

Derive branch name from app name in SEED.md: "Task Manager" → `build/task-manager`

---

## Step 5: Implement

Use `superpowers:subagent-driven-development` to execute the plan.

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
- Use the `frontend-design` skill before any UI work
- Use `context7` MCP for any external API documentation
- Do not add manual cache calls to model methods — caching is automatic
- JS safety: NEVER use `element.innerHTML = userValue` (XSS). ALWAYS use `element.textContent` for user-supplied text. ALWAYS use `createElement` for structured HTML.

---

## Step 5b: Stripe Webhook Registration (if SEED.md has Payments checked)

If `[x] Payments (Stripe)` is in SEED.md:

1. Read `APP_URL` from `.env`. If empty, STOP:
   > "APP_URL is not set. Set it to your production domain before registering the Stripe webhook."
2. Register webhook via Stripe MCP: endpoint `${APP_URL}/api/stripe_webhook`
3. Start local listener: `stripe listen --forward-to http://localhost:[APP_PORT]/api/stripe_webhook`
4. Extract local webhook secret → write to `.env` as `STRIPE_WEBHOOK_SECRET`
5. Fire test event: `stripe trigger payment_intent.succeeded`
6. Verify handler returns 200 in `docker compose logs app`
7. Stop listener, restore production secret to `.env`

---

## Step 6: Security Analysis

Run the `/security:analyze` command on `src/app/`.

---

## Step 7: Security Fixes (if needed)

If Critical, High, or Medium findings exist:
1. Write a targeted fix plan
2. Execute fixes
3. Re-run `/security:analyze`

---

## Step 8: Pre-Completion Verification

Use `superpowers:verification-before-completion`.

Verify:
- **Features:** All SEED.md features implemented? Auth-required pages call `requireAuth()`? No placeholder text?
- **CRUD:** If a create form exists, do edit and delete exist?
- **Architecture:** Tables via `execute_sql`? Models via `create_model`? No raw SQL in handlers? JS never uses `innerHTML` with user data?
- **Design:** `frontend-design` invoked? Titles set? Mobile-responsive?
- **App:** `docker compose logs app` shows no errors?
- **Environment:** New env vars documented in `env.example`? No hardcoded secrets?

---

## Step 9: Done

Report to the developer:

> **Build complete.**
>
> App running at: `http://localhost:[APP_PORT]`
> Branch: `build/[app-name]`
> Security report: `.security/SECURITY_REPORT.md`
>
> Next steps: review the running app, then run `/launch` to go live.
