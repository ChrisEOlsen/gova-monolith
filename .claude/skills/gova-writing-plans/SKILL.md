---
name: gova-writing-plans
description: Use when you have an approved GOVA design (a spec document, or an approved design from the Small path) and need an implementation plan of `gova` command tasks, before touching any scaffolding.
---

# Writing Plans

## Overview

Write a comprehensive implementation plan assuming the engineer has zero context for this codebase. Document everything they need to know: which `gova` command scaffolds each feature, which files get customized after, how to verify it. Give them the whole plan as bite-sized tasks. DRY. YAGNI. Frequent commits.

Assume they are a skilled developer, but know almost nothing about the GOVA toolset. Scaffold-generated code already has tests from its scaffold call — verification means running the right `gova` command, confirming the generated files, a clean `docker compose restart app`, and `go test ./...` passing. Hand-customized logic gets its own test per Step 2b below.

## Specify Contracts, Not Bodies

**The plan specifies what is not inferable. It does not pre-write the implementation.**

Writing the customization code into the plan and then having an implementer transcribe it means the code is generated twice — once at planning cost, once at implementation cost — and the plan balloons to several times the size of the spec it came from. That is the single largest source of slow builds in this stack. Do not do it.

**Verbatim in the plan — these are contracts the implementer cannot guess:**
- The exact `gova` command with exact flags
- Exact file paths
- Exact names crossing a task boundary: route paths, model method names, field names, table names, JS element IDs
- Exact literal values the spec fixes: user-facing copy, error codes, defaults, limits
- Any logic that is genuinely non-obvious: a security-sensitive check, a non-trivial algorithm, an ordering or concurrency requirement

**Described, not written — the implementer writes this once, at implementation time:**
- The body of a customization whose behavior follows from the interfaces above ("filter the list to `status = 'active'` before rendering", "add a delete button per row calling `del('/api/v1/projects/:id')` and re-running `loadList()` on success")
- Standard rendering, error display and form wiring — `./gova resource` already emits these; describe only what differs
- Test bodies (see Step 2b) — state what the test must prove, not its source

A customization step is well-specified when a competent implementer with the task brief, the interfaces block, and `CLAUDE.md` can write exactly one reasonable implementation. If two reasonable implementations differ in a way that matters, that difference is a contract — pin it. If they differ only in style, let the implementer choose.

**Announce at start:** "I'm using the gova-writing-plans skill to create the implementation plan."

**Context:** The feature branch should already exist (created via `/build` Step 4 — `git checkout -b build/<app-name>` in the main checkout, no worktree).

**Save plans to:** `docs/plans/YYYY-MM-DD-<feature-name>.md`

**Input:** on the Standard path this is a committed spec under `docs/specs/`. On the Small path there is no spec file — the approved design is the conversation, and this plan is the only written artifact, so it carries the user review gate that the spec would otherwise hold. Ask the user to review the saved plan before invoking `gova-build-execution`.

## Cover the whole design

One plan covers everything the approved design asks for. Do not split an app
across several plans, and do not quietly leave a feature for "later" — if the
design has eight resources, the plan has eight resources.

## Plan Size

The plan is a task list with contracts, not a second copy of the implementation. A single-feature plan is typically under 150 lines; a whole application, several hundred. If a plan is running several times the length of its spec, you are pre-writing implementation code — go back to "Specify Contracts, Not Bodies" and cut it. Length is a symptom, not a target: do not pad a short plan, and never drop a feature to hit a length.

## File Structure

Before defining tasks, map out which files will be created or modified and what each one is responsible for.

- Design units with clear boundaries: model files, handler files, JS modules, one per feature.
- Files that change together should live together. Split by feature, not by technical layer.
- In existing codebases, follow established patterns (`./gova inspect`). If a file you're modifying has grown unwieldy, including a split in the plan is reasonable.

This structure informs how you split the work into tasks. Each task should produce self-contained changes that make sense independently.

**Keep tasks file-disjoint.** The executor batches up to 3 implementers that
author concurrently (see `gova-build-execution` § The model), and two tasks
editing the same file would overwrite each other. If two features genuinely need
the same file, fold them into one task.

Also order tasks by dependency: a task's `Consumes` must be `Produces`d by an
earlier one. Tasks with no edge between them can share a batch.

## Task Right-Sizing

A task is one implementer's assignment and one reviewer's gate. One feature —
its table, its `./gova` command, its customization — is usually one task, with
setup and migration folded into the task whose deliverable needs them.

Beyond that, size tasks by your own read of the work. There is no line count and
no complexity rubric: a task ends with something independently verifiable (the
page loads, the endpoint returns the right shape), and that is the only rule.

## Steps within a task

**Each step is one action:**
- "Call the gova command" - step
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
gova command first for every feature file).]

---
```

## Task Structure

````markdown
### Task N: [Feature Name]

**Files:**
- Table: `feature_names` (via `./gova sql`)
- Scaffold: `./gova resource -name feature_name -fields ...` — model, five CRUD handlers, and a page with a create form and delete buttons, all self-registered in api.json + routes_gen.go + pages_gen.go
- Modify: `src/app/static/js/feature_names.js` — [what customization is needed]
- Modify: `src/app/handlers/feature_name_resource.go` — [what customization is needed, if any]

**Interfaces:**
- Consumes: [what this task uses from earlier tasks — exact model/route names]
- Produces: [what later tasks rely on — exact routes, model method names.
  A task's implementer sees only their own task; this block is how they
  learn the names neighboring tasks use.]

- [ ] **Step 1: Scaffold** *(the executor runs this before dispatch)*

```bash
./gova sql -query "CREATE TABLE feature_names (id INTEGER PRIMARY KEY, name TEXT NOT NULL, status TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);"
./gova resource -name feature_name -fields name:string,status:string
```

Generates `models/FeatureName.go`, `handlers/feature_name_resource.go`,
`static/pages/feature_names.html`, `static/js/feature_names.js` and their tests.

- [ ] **Step 2: Customize**

[Per "Specify Contracts, Not Bodies": state the behavior required, and pin the
exact names, literals, and endpoints it must use. Show code only where the logic
is non-obvious — a security check, a non-trivial algorithm, an ordering
requirement. Otherwise the implementer writes it.]

- [ ] **Step 2b: Write a test for the custom behavior** (only if this task hand-writes logic beyond the scaffold — a bespoke `gova handler` stub, or a scaffolded handler customized past its generated behavior; generated code already has tests from the scaffold itself)

[State what the test must prove — the input, the expected status and response
shape. Do not write the test source. Convention: same `_test.go` file as the
generated tests, `httptest` against the handler, `db.OpenTest` for any db-touching
test.]

*(The executor runs the `gova` command before dispatch, verifies the batch, and
commits — those are not task steps.)*
````

## No Placeholders

Every step must contain the actual content an engineer needs. These are **plan failures** — never write them:
- "TBD", "TODO", "implement later", "fill in details"
- "Add appropriate error handling" / "add validation" / "handle edge cases" — these name a category without saying which errors, which fields, or which cases
- "Similar to Task N" (restate the contract — the engineer may be reading tasks out of order, and sees only their own brief)
- An gova command with placeholder or omitted arguments
- References to models, routes, or fields not defined in any task

A described customization body is **not** a placeholder — see "Specify Contracts,
Not Bodies". The test is whether the description pins the behavior: "filter to
`status = 'active'`" is specified; "filter appropriately" is a placeholder.

## Remember
- Exact file paths always
- Exact gova commands with exact arguments — never abbreviated, never a placeholder
- Contracts verbatim, bodies described — the implementer writes the code once
- DRY, YAGNI, frequent commits
- Every feature task starts with a `gova` command — never "implement X handler" as a first step
- Generated code already has tests from its scaffold call — only plan a test-writing step for hand-customized logic (Step 2b)

## Self-Review

After writing the complete plan, look at the source requirements with fresh eyes and check the plan against them. This is a checklist you run yourself — not a subagent dispatch.

**1. Requirement coverage:** Skim every requirement in the spec (or the approved design, on the Small path). Can you point to a task that implements it? A requirement with no task is a gap, not a deferral — add the task.

**2. Placeholder scan:** Search your plan for red flags — any of the patterns from the "No Placeholders" section above. Fix them.

**3. Naming consistency:** Do the model names, route paths, and field names you used in later tasks match what you defined in earlier tasks? A model called `Project` in Task 3 but `Projects` in Task 7 is a bug.

**4. CRUD completeness:** If a create form exists for a feature, does the plan also cover edit and delete?

**5. Contract vs body:** Scan each customization step. Is anything there a full implementation the implementer could have written from the interfaces? Cut it to the contract. Conversely, is any step's behavior open to two materially different implementations? Pin it.

If you find issues, fix them inline. No need to re-review — just fix and move on. If you find a requirement with no task, add the task.

## Execution Handoff

After saving the plan:

> "Plan complete and saved to `docs/plans/<filename>.md`. Executing with gova-build-execution — fresh subagent per task, batched, review after each."

On the Small path, ask for the user's review of the plan first (see **Input** above) — it is the only written artifact, so it carries the review gate.

**REQUIRED SUB-SKILL:** Use `gova-build-execution` — fresh subagent per task + review.
