---
name: gova-writing-plans
description: Use when you have an approved GOVA design spec and need an implementation plan of MCP-tool-call tasks, before touching any scaffolding.
---

# Writing Plans

## Overview

Write a comprehensive implementation plan assuming the engineer has zero context for this codebase. Document everything they need to know: which MCP tool scaffolds each feature, which files get customized after, how to verify it. Give them the whole plan as bite-sized tasks. DRY. YAGNI. Frequent commits.

Assume they are a skilled developer, but know almost nothing about the GOVA toolset. Scaffold-generated code (models, handlers, auth) already has tests from its scaffold call — verification means calling the right MCP tool, confirming the generated files, a clean `docker compose restart app`, and `go test ./...` passing. Hand-customized logic gets its own test per Step 3b below.

**Announce at start:** "I'm using the gova-writing-plans skill to create the implementation plan."

**Context:** The feature branch should already exist (created via `/build` Step 4 — `git checkout -b build/<app-name>` in the main checkout, no worktree).

**Save plans to:** `docs/superpowers/plans/YYYY-MM-DD-<feature-name>.md`

## Scope Check

If the spec covers multiple independent subsystems, it should have been broken into sub-project specs during brainstorming. If it wasn't, suggest breaking this into separate plans — one per subsystem. Each plan should produce working, testable software on its own.

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

[The spec's project-wide requirements — auth required?, external integrations,
naming and copy rules — one line each, with exact values copied verbatim from
the spec. Every task's requirements implicitly include this section, plus the
Critical Constraints in CLAUDE.md (no raw SQL in handlers, no innerHTML with
user data, MCP tool first for every feature file).]

---
```

## Task Structure

````markdown
### Task N: [Feature Name]

**Files:**
- Table: `feature_name` (via `execute_sql`)
- Scaffold: `scaffold_list(name='feature_name', fields=[...])` — generates model, handler, `.html`, `.js`
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
scaffold_list(name='feature_name', fields=['name:string', 'status:string'])
```

- [ ] **Step 2: Verify the generated files**

Check `src/app/models/feature_name.go`, `handlers/feature_name.go`,
`static/pages/feature_name.html`, `static/js/feature_name.js` were created.

- [ ] **Step 3: Customize**

[Exact edits needed — show the code, not a description of the code]

- [ ] **Step 3b: Write a test for the custom behavior** (only if this task hand-writes logic beyond the scaffold — a bespoke `create_handler`/`create_page` stub, or a scaffolded handler customized past its generated behavior; generated CRUD/auth code already has tests from the scaffold call itself)

[Exact test code — same `_test.go` file convention as the generated tests: `httptest` against the handler, `db.OpenTest` for any db-touching test]

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
- "Add appropriate error handling" / "add validation" / "handle edge cases"
- "Similar to Task N" (repeat the code — the engineer may be reading tasks out of order)
- Steps that describe what to do without showing how (code blocks required for code steps)
- References to models, routes, or fields not defined in any task

## Remember
- Exact file paths always
- Complete code in every step — if a step changes code, show the code
- Exact MCP tool calls with exact arguments
- DRY, YAGNI, frequent commits
- Every feature task starts with an MCP scaffold call — never "implement X handler" as a first step
- Generated CRUD/auth code already has tests from its scaffold call — only plan a test-writing step for hand-customized logic (Step 3b)

## Self-Review

After writing the complete plan, look at the spec with fresh eyes and check the plan against it. This is a checklist you run yourself — not a subagent dispatch.

**1. Spec coverage:** Skim each section/requirement in the spec. Can you point to a task that implements it? List any gaps.

**2. Placeholder scan:** Search your plan for red flags — any of the patterns from the "No Placeholders" section above. Fix them.

**3. Naming consistency:** Do the model names, route paths, and field names you used in later tasks match what you defined in earlier tasks? A model called `Project` in Task 3 but `Projects` in Task 7 is a bug.

**4. CRUD completeness:** If a create form exists for a feature, does the plan also cover edit and delete?

If you find issues, fix them inline. No need to re-review — just fix and move on. If you find a spec requirement with no task, add the task.

## Execution Handoff

After saving the plan:

> "Plan complete and saved to `docs/superpowers/plans/<filename>.md`. Executing with gova-build-execution — fresh subagent per task, review between tasks."

**REQUIRED SUB-SKILL:** Use `gova-build-execution` — fresh subagent per task + review.
