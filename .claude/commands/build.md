---
description: Build a new GOVA application from SEED.md — spec to running app
---

Run the GOVA build workflow. Read this file completely before acting.

## 1. Validate

Read `SEED.md`. If it is empty or still placeholder text, STOP and ask the
developer to fill it in.

Read `.env`. If `SESSION_SECRET` is still `change-me-to-32-random-bytes-before-use`,
STOP:

> "SESSION_SECRET in .env is still the placeholder. Generate one before
> building: `openssl rand -hex 32`"

## 2. Brainstorm

Use the `gova-brainstorm` skill with `SEED.md` as input. Design everything
`SEED.md` asks for — building a subset and calling it done is the failure mode
here. Wait for approval before continuing.

## 3. Plan

Use the `gova-writing-plans` skill. The plan's tasks are **gova commands**, not
hand-written Go or JS. It specifies contracts — exact commands, paths, and the
names crossing task boundaries — and describes customization bodies rather than
pre-writing them.

## 4. Branch

`git checkout -b build/<app-name>` in the main checkout. **No worktrees** — see
`CLAUDE.md` § Parallel Builds for why.

## 5. Implement

Use `gova-build-execution`. It scaffolds each batch itself, dispatches up to 3
implementers to author concurrently, then verifies and commits — see its § The
model for why the split is what makes that safe.

Every subagent works under `CLAUDE.md` — the Mandatory Scaffolding Rule, the
Critical Constraints, and `docs/API-CONTRACT.md`. Do not restate them in the
dispatch; point at them.

**Design bar — build it slick.** Every page should look like a designed product,
not a scaffold with default styling:

- **Palette:** one accent color used deliberately, a neutral scale for the rest.
- **Type:** hierarchy through weight *and* size. Tight line-height on headings.
- **Spacing:** one consistent scale, applied generously. Cramped layouts are the
  clearest tell of a scaffold.
- **Motion:** transitions on hover, focus and state changes; entrance animation
  on lists and modals. Nothing that jitters or blocks input. Wrap it in
  `@media (prefers-reduced-motion: reduce)`.
- **Interaction states:** visible hover, focus, active and disabled on
  everything interactive. Style focus rings; never remove them.
- **Empty, loading and error states are designed, not blank.**

Tailwind utilities plus CSS transitions cover all of it — no framework, no CDN,
no animation library.

Use the `context7` MCP for external API documentation; fall back to web search
if it is not connected.

## 5b. Stripe webhook (only if SEED.md checks Payments)

The webhook is an ordinary endpoint at `/api/v1/stripe_webhook`, created with
`./gova handler` in step 5. It needs no CSRF exemption: it arrives with no
cookies, which `middleware.CSRF` already lets through.

1. Read `APP_URL` from `.env`. If empty, STOP and ask for the production domain.
2. Register the webhook via the Stripe MCP at `${APP_URL}/api/v1/stripe_webhook`.
3. `stripe listen --forward-to http://localhost:[APP_PORT]/api/v1/stripe_webhook`
4. Write the local signing secret to `.env` as `STRIPE_WEBHOOK_SECRET`.
5. `stripe trigger payment_intent.succeeded`, confirm a 200 in
   `docker compose logs app`.
6. Stop the listener and restore the production secret.

## 6. Security audit

Run `/security:analyze` (Claude Code) or `/security-analyze` (opencode) on
`src/app/`. Fix any Critical, High or Medium finding, then re-run it.

## 7. Verify

**No completion claim without fresh evidence.** If you have not run the check in
this message, you cannot claim it passes. Run each, read the output, then state
the result:

- **Features:** every SEED.md feature implemented, no placeholder text. Read the
  files; do not infer from the plan.
- **CRUD:** if a create form exists, do edit and delete?
- **Architecture:** grep for `innerHTML` and for `db.Query`/`db.Exec` outside
  `models/`. Confirm, don't assume.
- **Tests:** `docker compose exec app go test ./...` — all passing. A failing
  test blocks completion.
- **App:** `docker compose logs app` — no errors.
- **Design:** load the pages and look. Does it meet the step 5 bar?
- **Environment:** new env vars documented in `env.example`, no hardcoded secrets.

Fix and re-run the specific check that failed before moving on.

## 8. Merge decision

Ask:

> "Build complete on `build/<app-name>`. Merge to main now, or leave the branch
> for you to review first?"

If merging: `git checkout main && git merge build/<app-name> --no-edit && git branch -d build/<app-name>`.
Local only — do not push unless asked.

## 9. Report

> **Build complete.** App at `http://localhost:[APP_PORT]`.
> Branch: `build/<app-name>` [merged | left for review].
> Security report: `.security/SECURITY_REPORT.md`.
> Next: review the app, then `/launch`.
