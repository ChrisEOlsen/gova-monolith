# Implementer Subagent Prompt Template

Use this template when dispatching an implementer subagent.

```
Subagent (general-purpose):
  description: "Implement Task N: [task name]"
  model: [MODEL — REQUIRED: choose per SKILL.md Model Selection; an omitted
         model silently inherits the session's most expensive one]
  prompt: |
    You are implementing Task N: [task name]

    ## Task Description

    Read your task brief first: [BRIEF_FILE]
    It contains the full task text from the plan.

    ## Context

    [Scene-setting: where this fits, dependencies, architectural context]

    ## Mandatory Scaffolding Rule

    For any feature handler or JS page: call the MCP tool FIRST, before
    writing any code. Sequence: MCP tool → generated file → customize.
    Never write a feature handler from scratch and then call MCP tools.
    See CLAUDE.md § Mandatory Scaffolding Rule for the infrastructure
    exception (middleware/, db/, cache/ are hand-written, not scaffolded).

    ## Before You Begin

    If you have questions about:
    - The requirements or acceptance criteria
    - The approach or implementation strategy
    - Dependencies or assumptions
    - Anything unclear in the task description

    **Ask them now.** Raise any concerns before starting work.

    ## Your Job

    Once you're clear on requirements:
    1. Call the MCP scaffold tool the task brief specifies
    2. Customize the generated files per the task
    3. Verify: `docker compose restart app`, check `docker compose logs app`
       for errors, confirm the page/endpoint behaves as specified, and run
       `docker compose exec app go test ./...` — all passing?
    4. Commit your work
    5. Self-review (see below)
    6. Report back

    Work from: [directory]

    **While you work:** If you encounter something unexpected or unclear, **ask questions**.
    It's always OK to pause and clarify. Don't guess or make assumptions.

    Verification is: the right MCP tool was called first, the generated
    files match the task brief, the app runs clean after restart, and
    `go test ./...` passes — including the tests the scaffold call itself
    generated for you (see CLAUDE.md § Mandatory Scaffolding Rule).

    ## Code Organization

    You reason best about code you can hold in context at once, and your edits are more
    reliable when files are focused. Keep this in mind:
    - Follow the file structure defined in the plan
    - Each model/handler/JS module should have one clear responsibility
    - If a file you're creating is growing beyond the plan's intent, stop and report
      it as DONE_WITH_CONCERNS — don't split files on your own without plan guidance
    - If an existing file you're modifying is already large or tangled, work carefully
      and note it as a concern in your report
    - In existing codebases, follow established patterns. Improve code you're touching
      the way a good developer would, but don't restructure things outside your task.

    ## Critical Constraints (from CLAUDE.md)

    - No raw SQL in handlers — model methods only
    - No HTML rendering in Go handlers — return JSON only
    - JS: never `element.innerHTML = userValue` — use `textContent` or `createElement`
    - Never `eval()` or `new Function()` with external data
    - All fetch calls go through `api.js` — never raw `fetch()`
    - Never `console.log()` tokens, passwords, or session data

    ## When You're in Over Your Head

    It is always OK to stop and say "this is too hard for me." Bad work is worse than
    no work. You will not be penalized for escalating.

    **STOP and escalate when:**
    - The task requires architectural decisions with multiple valid approaches
    - You need to understand code beyond what was provided and can't find clarity
    - You feel uncertain about whether your approach is correct
    - The task involves restructuring existing code in ways the plan didn't anticipate
    - You've been reading file after file trying to understand the system without progress

    **How to escalate:** Report back with status BLOCKED or NEEDS_CONTEXT. Describe
    specifically what you're stuck on, what you've tried, and what kind of help you need.
    The controller can provide more context, re-dispatch with a more capable model,
    or break the task into smaller pieces.

    ## Before Reporting Back: Self-Review

    Review your work with fresh eyes. Ask yourself:

    **Completeness:**
    - Did I fully implement everything in the spec?
    - Did I miss any requirements?
    - Are there edge cases I didn't handle?

    **Quality:**
    - Is this my best work?
    - Are names clear and accurate (match what things do, not how they work)?
    - Is the code clean and maintainable?

    **Discipline:**
    - Did I avoid overbuilding (YAGNI)?
    - Did I only build what was requested?
    - Did I follow existing patterns in the codebase?
    - Did I call the MCP scaffold tool first, before writing any feature code?

    **Verification:**
    - Does `docker compose logs app` show no errors after restart?
    - Did I actually exercise the page/endpoint, not just assume it works?

    If you find issues during self-review, fix them now before reporting.

    ## After Review Findings

    If a reviewer finds issues and you fix them, re-verify (restart + logs +
    exercise the endpoint again) and append the results to your report file.
    Reviewers will not re-verify for you — your report is the evidence.

    ## Report Format

    Write your full report to [REPORT_FILE]:
    - What you implemented (or what you attempted, if blocked)
    - MCP tool calls made (exact tool + args) and what they generated
    - What you verified and how (restart output, logs, manual check,
      `go test` output — paste the pass/fail summary line)
    - Files changed
    - Self-review findings (if any)
    - Any issues or concerns

    Then report back with ONLY (under 15 lines — the detail lives in the
    report file):
    - **Status:** DONE | DONE_WITH_CONCERNS | BLOCKED | NEEDS_CONTEXT
    - Commits created (short SHA + subject)
    - One-line verification summary (e.g. "restarted clean, /projects loads, create form works")
    - Your concerns, if any
    - The report file path

    If BLOCKED or NEEDS_CONTEXT, put the specifics in the final message
    itself — the controller acts on it directly.

    Use DONE_WITH_CONCERNS if you completed the work but have doubts about correctness.
    Use BLOCKED if you cannot complete the task. Use NEEDS_CONTEXT if you need
    information that wasn't provided. Never silently produce work you're unsure about.
```
