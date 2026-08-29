---
name: gova-brainstorm
description: Use before writing an implementation plan for a GOVA build — classifies the scale of the work, then turns SEED.md into an approved design through batched collaborative dialogue, writing a spec document only when the scale warrants one.
---

# Brainstorming Ideas Into Designs

Help turn `SEED.md` into a fully formed design and spec through natural collaborative dialogue.

Start by understanding the current project context, classify the scale of the work, then ask batched questions to refine the idea. Once you understand what you're building, present the design and get user approval.

<HARD-GATE>
Do NOT invoke any implementation skill, write any code, or call any MCP scaffold tool until you have presented a design and the user has approved it. This applies to EVERY project regardless of perceived simplicity.
</HARD-GATE>

## Scale Gate — Run This First

Before asking anything, classify the work. This decides which path you take, and it is the difference between a 6-round-trip design cycle and a 2-round-trip one.

**Small** — a bugfix, a constraint or convention change, one endpoint, one model, or a change confined to existing infrastructure. No new subsystem.
→ **Skip the spec document.** Use plan mode for the dialogue (Claude Code's plan mode, or opencode's `plan` agent) — it is harness-enforced read-only, so the HARD-GATE holds mechanically rather than on your promise. Batch your questions, get the approach approved, then invoke `gova-writing-plans` for a short plan. Do not write to `docs/superpowers/specs/`.

**Standard** — a new feature with its own table, page, and routes; or two or more features that interact.
→ Full path below: questions → approaches → design → spec doc → plan.

**Large** — multiple independent subsystems.
→ Decompose first. Each sub-project gets its own spec → plan → implementation cycle. Brainstorm the first sub-project through the Standard path.

If the work sits between Small and Standard, take Small. A short plan that turns out to need a spec costs one extra round-trip; a full spec for a 100-line change costs six.

The HARD-GATE binds every path: no code, no MCP scaffold call, until the user approves the design (Standard/Large) or the plan (Small).

## Checklist

You MUST create a task for each of these items and complete them in order. Items marked **[Standard+]** are skipped on the Small path.

1. **Explore project context** — read `SEED.md`, check for an existing `docs/superpowers/specs/` or `docs/superpowers/plans/` history, check recent commits
2. **Classify scale** — Small, Standard, or Large, per the Scale Gate above. State which one you picked and why, in one line.
3. **Ask clarifying questions** — batched, not serial (see below); understand purpose/constraints/success criteria
4. **[Standard+] Propose 2-3 approaches** — with trade-offs and your recommendation
5. **Present design** — in sections scaled to their complexity, get user approval
6. **[Standard+] Write design doc** — save to `docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md` and commit
7. **[Standard+] Spec self-review** — quick inline check for placeholders, contradictions, ambiguity, scope (see below)
8. **[Standard+] User reviews written spec** — ask user to review the spec file before proceeding
9. **Transition to implementation** — invoke the `gova-writing-plans` skill to create an implementation plan

**The terminal state is invoking `gova-writing-plans`.** Do NOT start implementation work, make visual design decisions, or invoke any other skill yet. Visual polish is set by the design bar in `/build` Step 5 and happens at implementation time. The ONLY skill you invoke after brainstorming is `gova-writing-plans`.

## The Process

**Understanding the idea:**

- Check out the current project state first (`SEED.md`, docs, recent commits)
- Before asking detailed questions, assess scope: if `SEED.md` describes multiple independent subsystems, flag this immediately. Don't spend questions refining details of a project that needs to be decomposed first.
- If the project is too large for a single spec, help the user decompose into sub-projects: what are the independent pieces, how do they relate, what order should they be built? Then brainstorm the first sub-project through the normal design flow. Each sub-project gets its own spec → plan → implementation cycle.
- For appropriately-scoped projects, ask questions in **batches**, not one per message. Use the harness's batched-question tool (`AskUserQuestion` in Claude Code, `question` in opencode) — it takes several questions per call, each with a few options and optional multi-select. One call that resolves four decisions beats four calls that resolve one each; serial questioning is pure latency and is the single largest avoidable cost in this phase.
- Group a batch by theme so the answers are independently meaningful — data model in one batch, auth and integrations in the next. Do not batch a question whose options depend on the answer to another question in the same batch; that one waits for the following round.
- Prefer multiple choice when possible, but open-ended is fine — put it in the same batch as a plain question.
- Two batches is a normal budget for a Standard project, one for a Small one. If you are reaching for a third, you are designing by interview instead of proposing something concrete and letting the user correct it.
- Focus on understanding: purpose, constraints, success criteria, which SEED.md checkboxes (auth, payments) actually apply

**Exploring approaches — [Standard+]:**

- Propose 2-3 different approaches with trade-offs
- Present options conversationally with your recommendation and reasoning
- Lead with your recommended option and explain why
- On the Small path, skip this. There is rarely a second sensible way to change one endpoint, and enumerating one costs a round-trip. If a genuine fork exists, put it in the question batch as a multiple-choice option instead of as its own message.

**Presenting the design:**

- Once you believe you understand what you're building, present the design **in one message** and ask for approval once at the end. Do not gate section by section — that converts one round-trip into four, and the user can reject any section in a single reply just as easily.
- Scale each section to its complexity: a few sentences if straightforward, up to 200-300 words if nuanced
- Cover: data model (tables), pages/screens, auth requirements, external integrations (Stripe etc.), error handling
- Be ready to go back and clarify if something doesn't make sense

**Design for isolation and clarity:**

- Break the system into smaller units that each have one clear purpose, communicate through well-defined interfaces, and can be understood independently
- For each model/page, you should be able to answer: what does it do, how do you use it, and what does it depend on?
- Map each feature to the MCP scaffold tool that will build it (`scaffold_resource` for a full-CRUD resource — list/detail/create/update/delete + sort/filter; `scaffold_list` for a read-only list; `create_model`+`create_page` for a custom page; `scaffold_auth` for cookie+bearer auth) — this becomes the plan's task list later

**Working in existing codebases:**

- Explore the current structure before proposing changes (`inspect_app` via the `gova-builder` MCP if this is an incremental build on an existing app). Follow existing patterns.
- Don't propose unrelated refactoring. Stay focused on what serves the current goal.

## After the Design

**Small path — stop here.** No spec document, no spec self-review, no separate spec review gate. The approved design lives in the conversation and is carried forward into the plan, which is itself a committed artifact and gets its own user review. Go straight to **Implementation** below.

Everything between here and Implementation is **[Standard+]**.

**Documentation:**

- Write the validated design (spec) to `docs/superpowers/specs/YYYY-MM-DD-<topic>-design.md`
- Commit the design document to git

**Spec Self-Review:**
After writing the spec document, look at it with fresh eyes:

1. **Placeholder scan:** Any "TBD", "TODO", incomplete sections, or vague requirements? Fix them.
2. **Internal consistency:** Do any sections contradict each other? Does the data model match the feature descriptions?
3. **Scope check:** Is this focused enough for a single implementation plan, or does it need decomposition?
4. **Ambiguity check:** Could any requirement be interpreted two different ways? If so, pick one and make it explicit.

Fix any issues inline. No need to re-review — just fix and move on.

**User Review Gate:**
After the spec review loop passes, ask the user to review the written spec before proceeding:

> "Spec written and committed to `<path>`. Please review it and let me know if you want to make any changes before we start writing out the implementation plan."

Wait for the user's response. If they request changes, make them and re-run the spec review loop. Only proceed once the user approves.

**Implementation:**

- Invoke the `gova-writing-plans` skill to create a detailed implementation plan
- Do NOT invoke any other skill. `gova-writing-plans` is the next step.

## Key Principles

- **Scale the ceremony to the change** - The Scale Gate is the first decision, not an afterthought
- **Batch questions** - `AskUserQuestion` (Claude Code) or `question` (opencode), several at a time; never one question per message
- **Multiple choice preferred** - Easier to answer than open-ended when possible
- **YAGNI ruthlessly** - Remove unnecessary features from all designs
- **Explore alternatives** - Propose 2-3 approaches on Standard+ before settling
- **One approval gate per artifact** - Present the whole design, get approval once; don't gate section by section
- **Be flexible** - Go back and clarify when something doesn't make sense
