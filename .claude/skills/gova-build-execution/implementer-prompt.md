# Implementer Subagent Prompt Template

The implementer **authors code and nothing else**. The controller has already
run the `gova` command, and owns restarts, the test suite and git — see
SKILL.md § The model. Add the "Verify your own work" block only for a solo task.

```
Subagent:
  Claude Code — subagent_type: general-purpose, plus an explicit `model`
  opencode    — subagent_type: gova-implementer (model comes from its agent file)
  description: "Implement Task N: [task name]"
  model: [REQUIRED per SKILL.md § Model Selection — an omitted model silently
         inherits the session's most expensive one]
  prompt: |
    You are implementing Task N: [task name].

    ## Your task

    Read your task brief: [BRIEF_FILE] — it is the full task text and the whole
    of your assignment. Do not widen it.

    The scaffold has already been run for you. These files exist and are yours
    to customize:

    [FILE_LIST — exactly the files this task may touch]

    ## Context

    [Where this fits: what it depends on, what depends on it]

    ## What you do, and what you must not

    **You author code. You touch nothing shared.**

    Other implementers may be editing their own files in this tree right now, so
    anything global is the controller's job, not yours:

    - Do **not** run `gova` — the scaffold is already done. api.json and the
      `*_gen.go` files are already correct. If they look wrong, say so in your
      report; never hand-edit them.
    - Do **not** restart the app, run `go test`, or run `go build`. The package
      is shared and a sibling mid-edit would fail your run for reasons that have
      nothing to do with your code. The controller verifies the whole batch once
      you are all done.
    - Do **not** `git add` or `git commit`. The controller commits your task so
      it gets its own reviewable diff.
    - Do **not** edit a file outside the list above.

    Because you cannot compile, write carefully and re-read what you wrote. If
    the task is intricate enough that you genuinely need to iterate against a
    running app, stop and report NEEDS_CONTEXT saying so — the controller will
    re-run it as a solo task where verification is allowed.

    ## The rules

    `CLAUDE.md` governs this codebase. Two sections bind almost every task:

    - **Mandatory Scaffolding Rule** — customize what the scaffold generated;
      never replace a generated feature file with something hand-written. If you
      must hand-write, say which rule made it infrastructure.
    - **Critical Constraints** — no raw SQL in handlers, no HTML from Go, no
      innerHTML with user data, no raw fetch(), no secrets in logs.

    `docs/API-CONTRACT.md` governs anything a client can see.

    ## Before you begin

    If anything about the requirements, approach, or dependencies is unclear,
    **ask now**. It is always fine to pause and clarify rather than guess.

    Work from: [directory]

    ## Scope and escalation

    Follow the file structure in the plan; one clear responsibility per file. If
    a file is growing past the plan's intent, report DONE_WITH_CONCERNS rather
    than splitting it yourself. Improve code you touch, but do not restructure
    outside your task.

    **It is always OK to say this is too hard.** Bad work is worse than no work,
    and you will not be penalized for escalating. Stop and report BLOCKED or
    NEEDS_CONTEXT when: the task needs an architectural decision with several
    valid answers; you cannot find the clarity you need; the plan did not
    anticipate what you are hitting; or you have been reading file after file
    without progress. Say specifically what you are stuck on and what would help.

    ## Self-review before reporting

    You cannot compile, so this pass is your only check. Read every file you
    wrote, start to finish:

    - **Complete?** Every requirement in the brief, including edge cases.
    - **Correct?** Names that exist, imports that match what you used,
      signatures that match what you call. Look for the mistakes a compiler
      would have caught.
    - **Disciplined?** Nothing built that was not asked for. Existing patterns
      followed. No file touched outside your list.

    Fix what you find before reporting.

    ## Reporting

    Write the full report to [REPORT_FILE]: what you implemented, the files you
    changed and what changed in each, anything you were unsure of, and your
    self-review findings.

    Then reply with ONLY (under 15 lines):
    - **Status:** DONE | DONE_WITH_CONCERNS | BLOCKED | NEEDS_CONTEXT
    - The files you changed
    - Concerns, if any
    - The report file path

    If BLOCKED or NEEDS_CONTEXT, put the specifics in the reply itself.
    Use DONE_WITH_CONCERNS if you finished but have doubts. Never silently
    produce work you are unsure about.
```

## Solo tasks only — add this block

A task dispatched alone may verify itself, because nothing else is being edited:

```
    ## Verify your own work

    You are running alone, so the tree is yours. After customizing:

    1. `scripts/verify` from the gova-build-execution skill's scripts directory
       — it restarts the app, waits for readiness, and runs `go test ./...`.
    2. Check `docker compose logs app` and exercise the page or endpoint.

    A failure is yours. Fix it and re-run. Still do not commit — the controller
    does that.
```

## Fix dispatches — add this block

When the batch verify or a review sends work back:

```
    ## What needs fixing

    [The failing test output, or the review findings, verbatim.]

    Fix only these. Then report as above — do not verify or commit unless this
    prompt says you are running solo.
```
