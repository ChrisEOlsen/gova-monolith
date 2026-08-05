---
name: gova-writing-plans
description: Use when you have an approved GOVA design (a spec document, or an approved design from the Small path) and need an implementation plan of MCP-tool-call tasks, before touching any scaffolding.
---

# Writing Plans

## Overview

Write a comprehensive implementation plan assuming the engineer has zero context for this codebase. Document everything they need to know: which MCP tool scaffolds each feature, which files get customized after, how to verify it. Give them the whole plan as bite-sized tasks. DRY. YAGNI. Frequent commits.

Assume they are a skilled developer, but know almost nothing about the GOVA toolset. Scaffold-generated code (models, handlers, auth) already has tests from its scaffold call — verification means calling the right MCP tool, confirming the generated files, a clean `docker compose restart app`, and `go test ./...` passing. Hand-customized logic gets its own test per Step 3b below.

## Specify Contracts, Not Bodies

**The plan specifies what is not inferable. It does not pre-write the implementation.**

Writing the customization code into the plan and then having an implementer transcribe it means the code is generated twice — once at planning cost, once at implementation cost — and the plan balloons to several times the size of the spec it came from. That is the single largest source of slow builds in this stack. Do not do it.

**Verbatim in the plan — these are contracts the implementer cannot guess:**
- The exact MCP tool call with exact arguments (`execute_sql` SQL, `scaffold_resource(name=..., fields=[...])`)
- Exact file paths
- Exact names crossing a task boundary: route paths, model method names, field names, table names, JS element IDs
- Exact literal values the spec fixes: user-facing copy, error codes, defaults, limits
- Any logic that is genuinely non-obvious: a security-sensitive check, a non-trivial algorithm, an ordering or concurrency requirement

**Described, not written — the implementer writes this once, at implementation time:**
- The body of a customization whose behavior follows from the interfaces above ("filter the list to `status = 'active'` before rendering", "add a delete button per row calling `del('/api/v1/projects/:id')` and re-running `loadList()` on success")
- Standard rendering, standard error display, standard form wiring — these follow the Frontend Patterns in `CLAUDE.md`
- Test bodies (see Step 3b) — state what the test must prove, not its source

A customization step is well-specified when a competent implementer with the task brief, the interfaces block, and `CLAUDE.md` can write exactly one reasonable implementation. If two reasonable implementations differ in a way that matters, that difference is a contract — pin it. If they differ only in style, let the implementer choose.

**Announce at start:** "I'm using the gova-writing-plans skill to create the implementation plan."

**Context:** The feature branch should already exist (created via `/build` Step 4 — `git checkout -b build/<app-name>` in the main checkout, no worktree).

**Save plans to:** `docs/superpowers/plans/YYYY-MM-DD-<feature-name>.md`

**Input:** on the Standard/Large path this is a committed spec under `docs/superpowers/specs/`. On the Small path (see `gova-brainstorm` § Scale Gate) there is no spec file — the approved design is the conversation, and this plan is the only written artifact, so it carries the user review gate that the spec would otherwise hold. Ask the user to review the saved plan before invoking `gova-build-execution`.

## Scope Check

If the spec covers multiple independent subsystems, it should have been broken into sub-project specs during brainstorming. If it wasn't, suggest breaking this into separate plans — one per subsystem. Each plan should produce working, testable software on its own.

## Plan Size

The plan is a task list with contracts, not a second copy of the implementation. A single-feature plan is typically under 150 lines; a multi-feature build under 600. If a plan is running several times the length of its spec, you are pre-writing implementation code — go back to "Specify Contracts, Not Bodies" and cut it. Length is a symptom, not a target: do not pad a short plan, and do not truncate a genuinely large one.

## File Structure

Before defining tasks, map out which files will be created or modified and what each one is responsible for. This is where decomposition decisions get locked in.

- Design units with clear boundaries: model files, handler files, JS modules, one per feature.
- Files that change together should live together. Split by feature, not by technical layer.
- In existing codebases, follow established patterns (`inspect_app`). If a file you're modifying has grown unwieldy, including a split in the plan is reasonable.

This structure informs the task decomposition. Each task should produce self-contained changes that make sense independently.

## Task Right-Sizing

A task is the smallest unit that carries its own verification cycle and is worth a fresh reviewer's gate. One feature (one `execute_sql` + one `scaffold_*` call + its customization) is usually one task. Fold setup, migration, and customization into the task whose deliverable needs them; split only where a reviewer could meaningfully reject one task while approving its neighbor. Each task ends with an independently verifiable deliverable (page loads, endpoint returns the right shape).

## Bite-Sized Task Granularity

**Each step is one action (2-5 minutes):**
- "Call the MCP scaffold tool" - step
- "Verify the generated files" - step
- "Customize the generated handler/JS" - step
- "Restart the container and check logs" - step
- "Commit" - step

## Plan Document Header

**Every plan MUST start with this header:**

```markdown
# [Feature Name] Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: use gova-build-execution to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** [One sentence describing what this builds]

**Architecture:** [2-3 sentences about approach]

**Tech Stack:** GOVA Monolith — Go/chi, SQLite, vanilla JS, Tailwind

## Global Constraints

[The project-wide requirements — auth required?, external integrations,
naming and copy rules — one line each, with exact values copied verbatim from
the spec (or, on the Small path, from the approved design in the conversation).
Every task's requirements implicitly include this section, plus the Critical
Constraints in CLAUDE.md (no raw SQL in handlers, no innerHTML with user data,
MCP tool first for every feature file).]

---
```

## Task Structure

````markdown
### Task N: [Feature Name]

**Files:**
- Table: `feature_name` (via `execute_sql`)
- Scaffold: `scaffold_resource(name='feature_name', fields=[...])` for a full-CRUD resource (list/detail/create/update/delete + `?sort=`/`?filter=`), or `scaffold_list(...)` for a read-only list — generates model, handler(s), `.html`, `.js`, and self-registers routes in api.json + routes_gen.go
- Modify: `src/app/static/js/feature_name.js` — [what customization is needed]
- Modify: `src/app/handlers/feature_name.go` — [what customization is needed, if any]

**Interfaces:**
- Consumes: [what this task uses from earlier tasks — exact model/route names]
- Produces: [what later tasks rely on — exact routes, model method names.
  A task's implementer sees only their own task; this block is how they
  learn the names neighboring tasks use.]

- [ ] **Step 1: Call the MCP scaffold tool**

```
execute_sql(sql="CREATE TABLE feature_name (id INTEGER PRIMARY KEY, ...);")
# Full CRUD (list/detail/create/update/delete + sort/filter):
scaffold_resource(name='feature_name', fields=['name:string', 'status:string'])
# ...or scaffold_list(name='feature_name', fields=[...]) for a read-only list.
```

- [ ] **Step 2: Verify the generated files**

Check `src/app/models/feature_name.go`, `handlers/feature_name.go`,
`static/pages/feature_name.html`, `static/js/feature_name.js` were created.

- [ ] **Step 3: Customize**

[Per "Specify Contracts, Not Bodies": state the behavior required, and pin the
exact names, literals, and endpoints it must use. Show code only where the logic
is non-obvious — a security check, a non-trivial algorithm, an ordering
requirement. Otherwise the implementer writes it.]

- [ ] **Step 3b: Write a test for the custom behavior** (only if this task hand-writes logic beyond the scaffold — a bespoke `create_handler`/`create_page` stub, or a scaffolded handler customized past its generated behavior; generated CRUD/auth code already has tests from the scaffold call itself)

[State what the test must prove — the input, the expected status and response
shape. Do not write the test source. Convention: same `_test.go` file as the
generated tests, `httptest` against the handler, `db.OpenTest` for any db-touching
test.]

- [ ] **Step 4: Restart and verify**

Run: `docker compose restart app`
Check: `docker compose logs app` shows no errors; page loads at `/feature_name`

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add feature_name"
```
````

## No Placeholders

Every step must contain the actual content an engineer needs. These are **plan failures** — never write them:
- "TBD", "TODO", "implement later", "fill in details"
- "Add appropriate error handling" / "add validation" / "handle edge cases" — these name a category without saying which errors, which fields, or which cases
- "Similar to Task N" (restate the contract — the engineer may be reading tasks out of order, and sees only their own brief)
- An MCP tool call with placeholder or omitted arguments
- References to models, routes, or fields not defined in any task

A described customization body is **not** a placeholder — see "Specify Contracts,
Not Bodies". The test is whether the description pins the behavior: "filter to
`status = 'active'`" is specified; "filter appropriately" is a placeholder.

## Remember
- Exact file paths always
- Exact MCP tool calls with exact arguments — never abbreviated, never a placeholder
- Contracts verbatim, bodies described — the implementer writes the code once
- DRY, YAGNI, frequent commits
- Every feature task starts with an MCP scaffold call — never "implement X handler" as a first step
- Generated CRUD/auth code already has tests from its scaffold call — only plan a test-writing step for hand-customized logic (Step 3b)

## Self-Review

After writing the complete plan, look at the source requirements with fresh eyes and check the plan against them. This is a checklist you run yourself — not a subagent dispatch.

**1. Requirement coverage:** Skim each section/requirement in the spec (or the approved design, on the Small path). Can you point to a task that implements it? List any gaps.

**2. Placeholder scan:** Search your plan for red flags — any of the patterns from the "No Placeholders" section above. Fix them.

**3. Naming consistency:** Do the model names, route paths, and field names you used in later tasks match what you defined in earlier tasks? A model called `Project` in Task 3 but `Projects` in Task 7 is a bug.

**4. CRUD completeness:** If a create form exists for a feature, does the plan also cover edit and delete?

**5. Contract vs body:** Scan each customization step. Is anything there a full implementation the implementer could have written from the interfaces? Cut it to the contract. Conversely, is any step's behavior open to two materially different implementations? Pin it.

If you find issues, fix them inline. No need to re-review — just fix and move on. If you find a requirement with no task, add the task.

## Execution Handoff

After saving the plan:

> "Plan complete and saved to `docs/superpowers/plans/<filename>.md`. Executing with gova-build-execution — fresh subagent per task, review between tasks."

On the Small path, ask for the user's review of the plan first (see **Input** above) — it is the only written artifact, so it carries the review gate.

**REQUIRED SUB-SKILL:** Use `gova-build-execution` — fresh subagent per task + review.
