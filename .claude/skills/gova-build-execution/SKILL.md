---
name: gova-build-execution
description: Use to execute a GOVA implementation plan — dispatches a fresh implementer subagent per task, reviews spec compliance and code quality after each, and runs a final whole-branch review.
---

# GOVA Build Execution

Fresh implementer subagent per task, a task review after each, and one broad
whole-branch review at the end.

Subagents never inherit your session's history — you construct exactly the
context each one needs, which keeps them focused and preserves your own context
for coordination.

**Narrate at most one short line between tool calls.** The ledger and the tool
results carry the record.

**Do not pause between tasks.** Execute the whole plan. Stop only for a BLOCKED
status you cannot resolve, ambiguity that genuinely prevents progress, or
completion. "Should I continue?" wastes the developer's time.

## The model: parallel authoring, serial touching

Subagents **author code**. You own every shared resource. That split is what
makes parallelism safe without any locking.

Two tasks with disjoint file lists still collide on three shared globals — the
`app` container (a restart kills a sibling's test run), `go test ./...` (it
compiles the whole package, so a sibling mid-edit fails your run), and the git
index (`index.lock`). File-disjointness does not help with any of them. So
nobody but you touches them.

| | Implementer | You |
|---|---|---|
| `gova` commands | never | yes, before dispatch |
| Edit files | its own task's list only | no |
| Restart / `go test` | never | once per batch |
| `git add` / `commit` | never | once per task |

## The loop

1. Read the plan, note its Global Constraints, create a todo per task.
2. **Form a batch** of up to 3 tasks with no dependency edge between them —
   neither consumes what another produces (check the plan's `Interfaces:`
   blocks). A task whose input another task produces waits for the next batch.
3. **Scaffold the batch yourself**, serially: run each task's `gova` command.
   These take about a second each and go through the builder's own lock, so
   `api.json` and the `*_gen.go` files are complete and correct before any
   subagent starts.
4. **Dispatch the batch** — up to 3 implementers, concurrently. Each customizes
   only the files its scaffold produced.
5. **Verify once**: `scripts/verify`. If it fails, `go test` names the file, and
   every file belongs to exactly one task — re-dispatch that implementer with
   the failure text. Repeat until green.
6. **Commit per task**, so each gets its own reviewable diff. Record the BASE
   and HEAD for each.
7. **Review**, in parallel — reviewers are read-only and cannot collide.
   Critical/Important findings → fix subagent → re-verify → re-review.
8. Mark each task complete in the todos and the ledger, then start the next batch.
9. After the last batch: final whole-branch review against the commit the branch
   started from — the `code-review` skill (Claude Code) or `gova-architect` /
   `/review` (opencode). Findings → ONE fix subagent with the complete list →
   hand back to `/build` for the security audit.

## When to run a task alone

An implementer in a batch cannot compile — the package is shared, and a sibling
mid-edit would fail its build. It writes carefully and your batch verify is the
gate. That is the right trade for ordinary customization and the wrong one for
anything intricate.

**Run a task solo** — batch of one, and tell the implementer it may verify
itself — when it rewrites shared infrastructure, when its logic is subtle enough
that the author needs to iterate against a running app, or when it came back
BLOCKED once already.

**Batches of one are always valid.** The batching is an optimization; when in
doubt, serialize.

## Pre-flight

Before Task 1, scan the plan once for tasks that contradict each other or the
Global Constraints, and for anything the plan mandates that the review rubric
treats as a defect. Present everything you find as one batched question, before
execution begins — not one interrupt per discovery. If the scan is clean,
proceed without comment.

## Model selection

Use the least powerful model that can do the job.

- **Mechanical task** (one scaffold call, 1–2 files to customize): fast model.
- **Integration or judgment**: standard model.
- **Architecture, and the final whole-branch review**: the most capable model.
- **Reviews**: scale to the diff's size and risk.

**Always name the model explicitly** — an omitted one inherits your session's,
usually the most expensive.

**Turn count beats token price.** The cheapest models often take 2–3× the turns
on multi-step work and cost more overall. Mid-tier is the floor for reviewers,
and for any task with a customization step — plans specify contracts, not
bodies, so the implementer is authoring, not transcribing.

Under opencode the model is not a dispatch argument: use `subagent_type`
`gova-implementer`, `gova-reviewer` or `gova-architect`, whose models live in
`.opencode/agent/*.md`.

## Implementer statuses

- **DONE** → the files are edited but nothing is committed. Once the whole batch
  is DONE and verified, commit each task and build its review package from the
  BASE and HEAD you recorded.
- **DONE_WITH_CONCERNS** → read them. Correctness or scope concerns get
  addressed before review; observations get noted.
- **NEEDS_CONTEXT** → provide it, re-dispatch.
- **BLOCKED** → assess: missing context (provide it), needs more reasoning
  (stronger model), too large (split it), or the plan is wrong (escalate to the
  human). **Never** re-dispatch unchanged.

## Reviewer output

⚠️ "Cannot verify from diff" items do not block the review, but you must resolve
each before marking the task complete — you hold the cross-task context the
reviewer lacks. A confirmed gap is a failed spec review: send it back.

Dispatch fix subagents for Critical and Important findings. Record Minor ones in
the ledger and point the final review at that list to triage before merge.

A finding that conflicts with what the plan requires is the human's call:
present the finding and the plan text and ask which governs.

## Constructing dispatch prompts

- Hand over artifacts as **files**, not pasted text — everything you paste stays
  in your context for the rest of the session and is re-read every turn.
- A dispatch describes one task, not the session's history. Never paste
  accumulated prior-task summaries into a later dispatch.
- Copy the plan's binding requirements verbatim into `[GLOBAL_CONSTRAINTS]` —
  that block is the reviewer's attention lens. Process rules are already in the
  template.
- **Never tell a reviewer what not to flag** and never pre-rate a finding's
  severity. If you think something would be a false positive, let it be raised
  and adjudicate it.
- Do not ask a reviewer to re-verify what the implementer already verified on
  the same code.

## Durable progress

Conversation memory does not survive compaction; the ledger does.

- At skill start: `cat "$(git rev-parse --show-toplevel)/.gova-build/progress.md"`.
  Tasks marked complete there are DONE — resume at the first that is not.
- On a clean review, append one line:
  `Task N: complete (commits <base7>..<head7>, review clean)`.
- After compaction, trust the ledger and `git log` over your own recollection.

## Templates

- [implementer-prompt.md](implementer-prompt.md)
- [task-reviewer-prompt.md](task-reviewer-prompt.md)

## Never

- Start on `main` without explicit consent
- Skip a task review, or accept a report missing either verdict
- Move on with unfixed Critical/Important findings
- Put two tasks with a dependency edge in one batch
- Exceed 3 implementers in a batch
- Let an implementer run `gova`, restart the app, run the suite, or commit
- Dispatch a batch before scaffolding all of it yourself
- Commit a batch before `scripts/verify` is green
- Make a subagent read the whole plan file — hand it its brief
- Let implementer self-review replace actual review
- Re-dispatch a task the ledger already marks complete
