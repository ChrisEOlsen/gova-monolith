# GOVA Upgrade Summary — API-First for Native Clients

This document records the multi-build effort that made gova-monolith build web
apps whose API is a strict, self-describing contract, and made gova-ios translate
those apps by *reading* that contract instead of reverse-engineering source.

**The through-line:** the web app used to be the only consumer of its own API, and
lenient browser JavaScript hid a pile of assumptions that a strictly-typed native
(Swift) client cannot tolerate. Each build turned one of those hidden assumptions
into an explicit, verified contract — and the last builds made the iOS side simply
consume it.

Delivered as five sequential builds, each its own spec → plan → execute → review →
merge cycle:

| Build | What it did | Merge |
|---|---|---|
| 1 | API wire contract | `72533b6` |
| 2 | API manifest + auto-routing | `33779ee` |
| 3a | iOS consumes the manifest | gova-ios `9012234` |
| 3b | `scaffold_resource` full CRUD | `cdfb6ad` |
| 3c | Unified auth | `1cee596` |

---

## Problems and how we solved them

### Build 1 — the wire format lied by omission

| Problem | Why it mattered | Fix |
|---|---|---|
| An empty list serialized as `null` (`Data any` + `omitempty` on a nil slice) | JS shrugged (`res.data ?? []`); a Swift decoder expecting `[Item]` throws and the whole screen fails | Models initialize slices non-nil **and** the envelope reflects a nil slice to `[]` — a guard at both the source and the boundary |
| Timestamps marshaled as RFC3339 **nano** | Swift's `.iso8601` decoder rejects fractional seconds; JS `new Date()` accepts anything | A `models.Time` type pinned to RFC3339 UTC, second precision — decodes with stock `.iso8601` |
| Nullability died in the generator | The DB knew which columns were nullable; nothing published it, so iOS re-guessed from the SQL schema | The builder runs `PRAGMA table_info` and emits nullable columns as Go pointers (→ JSON `null` → Swift optional), with a guard that fails the tool on a field/schema mismatch |
| Errors were bare strings | A client couldn't distinguish auth-expiry from validation from conflict | Flat-sibling envelope: `error` stays a string, plus machine-readable `code` and per-field `fields` |
| Lists were unbounded | Mobile on cellular pulled the whole table | Always-on offset pagination, `meta{limit,offset,total}` |
| No versioning | Web deploys instantly; App Store builds don't | `/api/v1` prefix + a `_version` endpoint to assert against |

### Build 2 — nothing described the wire, and routes were hand-wired

| Problem | Fix |
|---|---|
| iOS reverse-engineered the API by parsing Go structs, grepping `main.go`, and reading JS | `src/app/api.json` became the single source of truth — every scaffold tool upserts its models + endpoints (types, nullability, auth, kind) into it |
| Routes were hand-pasted into `main.go` by the LLM; the served surface could drift from what existed | `handlers/routes_gen.go` is **generated** from `api.json`; `main.go` mounts it with one `RegisterGenerated(...)` call and is never hand-edited for a route again. The served manifest and the actual routes come from one file — they can't diverge |
| `inspect_app` returned prose | Now structured JSON (`{manifest, on_disk, divergence}`) that flags files drifting from the manifest |
| Per-endpoint auth was implicit | `auth:true` in the manifest generates a `RequireAuth` route wrap — auth is declarative |

### Build 3a — iOS still inferred instead of reading the contract

| Problem | Fix |
|---|---|
| `/export:mobile` read Go/JS/`main.go` to build the iOS spec — inference over source | It now runs a **deterministic, tested `python3` transform** over `api.json`, writing nullability, timestamp format, and auth to SEED.md as *stated facts*. No parsing, no MCP, no running server |
| The `_version` endpoint (Build 1) had no consumer | A pre-committed Swift `VersionGate` checks it at launch and blocks a too-old build — **failing open** on any error so a flaky network never bricks the app |

### Build 3b — a scaffolded resource could only be read

| Problem | Fix |
|---|---|
| `scaffold_list` generated a list endpoint only; no detail/create/update/delete, and the model had no `Update` | New `scaffold_resource` generates the full CRUD surface, self-registering all five endpoints — so 3a's export surfaces them with **zero change** (it groups endpoints by model regardless of kind) |
| A list couldn't be sorted or filtered server-side | Whitelisted `?sort=`/`?filter=`, with the injection-safety logic in a shared, hand-written, unit-tested `models/query.go` — only whitelisted column names are interpolated; values are always bound parameters |

### Build 3c — auth was a two-tool split-brain

| Problem | Fix |
|---|---|
| `scaffold_auth` (cookie) and `scaffold_mobile_auth` (bearer) were separate tools run in sequence; forget the second and mobile silently had no auth, papered over by a procedural reminder in the gova-ios workflow | `scaffold_auth` now emits **both** cookie and bearer auth in one run — all tables, all handlers, all six endpoints. `scaffold_mobile_auth` was removed. One command yields working web + mobile auth (proven live: cookie login + bearer `me_token` from a single run) |

---

## Bugs the review process caught (that green tests hid)

The build discipline — a fresh implementer per task, an independent per-task
review, and a whole-branch review on the most capable model — repeatedly caught
defects that passing tests did not:

- **Build 1:** a "fix" that didn't fix anything — a `Value()` UTC test asserted with
  `time.Time.Equal`, which ignores `Location`, so it passed with or without the
  normalization it was meant to guard. Re-fixed with a mutation-proven assertion.
- **Build 1 final review:** two **live breakages** — `auth.js` and the CSRF exemption
  still on unversioned `/api/` paths after the `/api/v1` migration — which would have
  404'd client auth and 403'd mobile login. They lived in hand-written infra files no
  per-task review covered.
- **Build 2 (a runtime bug behind green tests):** an implementer set the manifest path
  to `../api.json` to satisfy the package-dir *test* CWD — which would have made the
  endpoint read a nonexistent path *at runtime*. Fixed to the runtime-correct path with
  the tests adapting via `t.Chdir`, not the reverse.
- **Build 2 final review (a Critical):** mobile bearer endpoints were wrapped in
  session-cookie `RequireAuth`, which a bearer client can never satisfy → 401 for every
  mobile user, while tests stayed green because they called handlers directly. Fixed to
  `auth:false` (the handlers self-enforce the token), proven end-to-end.
- **Build 3b final review:** surfaced a pre-existing footgun — the generated model
  marshals *every* field to JSON, so a resource with a `password` column would expose
  its hash. Recorded as a scoped follow-up (not a 3b regression).

---

## Design principles that guided the changes

1. **The contract is data, not prose or inference.** `api.json` is one machine-readable
   source of truth; the served manifest, the generated routes, and the iOS export are all
   *derived* from it, so they cannot drift. Wherever a consumer had to *guess* (nullability,
   timestamp format, auth), we replaced the guess with a stated fact.

2. **Generate one thing from one source.** Routes and the served manifest both come from
   `api.json`; the iOS Generated Context comes from the same file via a deterministic script.
   A single source with multiple derived outputs eliminates a whole class of "these two
   descriptions disagree" bugs.

3. **Determinism over cleverness.** The iOS export is a tested `python3` script, not
   LLM-transcribed JSON, precisely because the spec required byte-identical output on an
   unchanged manifest. If a step must be reproducible, make it code with a test — not prose
   an agent re-interprets each run.

4. **Fail loud at the boundary, fail safe at runtime.** Bad scaffold input (a field that
   doesn't match the schema, a sort column not in the whitelist, a route conflict) errors
   immediately and writes nothing. But a shipped client that can't reach the version
   endpoint fails *open* — a flaky network must never brick the app. Loud where a human can
   fix it; safe where a user would be stranded.

5. **Security boundaries are hand-written and tested, not generated.** The sort/filter
   whitelist and the injection-safety logic live in a hand-written `models/query.go` with
   unit tests, not regenerated per resource — the one place SQL injection could enter is the
   one place we didn't template.

6. **Client-input errors tell the truth.** Every client error maps to an envelope `code` the
   contract actually defines (422 `validation_failed`, 404 `not_found`, …). We used 422 rather
   than 400 specifically because the code-mapping had no 400 entry — an honest `code` beats a
   convenient status.

7. **Two-way doors get a default; one-way doors get the human.** Public-by-default resources,
   `/api/v1`, fail-open version checks — all reversible defaults chosen to keep moving. The
   genuinely consequential forks (envelope shape, auth model, whether to merge login endpoints)
   were surfaced as explicit decisions.

8. **Verification is evidence, not assertion.** Every build ended with an end-to-end run
   against the *rebuilt* MCP image (templates are `go:embed`-ed at image-build time — a plain
   restart runs the stale binary, a trap that nearly masked an entire build), exercising the
   real tools and curling live endpoints, then reverting to a clean tree. "It compiles" and
   "the unit tests pass" were never accepted as "it works."

9. **Improve the code you touch, no further.** Stale tool descriptions, doc drift, and a
   mislabeled test were fixed as they surfaced during the work; unrelated refactors were left
   alone. The scope stayed on making *this* contract correct.

---

## Operational notes for future work

- **After any `src/builder/` change, rebuild the mcp image** (`docker compose up -d --build`),
  not just restart — the templates and tool set are embedded at image-build time.
- **The app rebuilds from bind-mounted source on `docker compose restart app`** — app-side
  changes need only a restart.
- **`api.json` and `routes_gen.go` are committed source, not build artifacts** — regenerated
  by the scaffold tools, upsert-only, no removal tool.
- **Deferred follow-ups** (recorded, non-blocking): the model template exposes `password`-typed
  fields in JSON (a shared-template security pass); per-field 422 create/update validation;
  filter operators beyond equality; web edit/delete UI; a possible `auth` enum
  (`session`/`bearer`/`none`) so the manifest is unambiguous for bearer endpoints.

---

## Field-review follow-up arc (Builds A–C)

After the first arc shipped, a real gova-monolith app was translated to a native iOS
app — and it **worked**. A review of that translation surfaced the friction that
remained: places where the contract stopped one layer short of what a strictly-typed
native client needs, plus a template correctness bug and an iOS verification gap. This
arc closed them, again as sequential spec → plan → subagent-execute → per-task review →
opus whole-branch review → merge cycles.

| Build | Repo | What it did | Merge |
|---|---|---|---|
| A | monolith | CSRF applies only to browser double-submit participants | `ec3be5c` |
| B-emit | monolith | Manifest carries the full request/response contract | `7c950bf` |
| B-consume | gova-ios | iOS reads the enriched contract | `734e321` |
| C | gova-ios | UI smoke test + device-deploy defaults | `975cb78` |

### Problems and how we solved them

**Build A — CSRF assumed every client was a browser.** The double-submit middleware
force-enrolled every mutating request: with no `csrf_token` cookie it *minted* one and
demanded a matching header a native client never sends → every write 403'd on a no-auth
native app. Fix: enforce the header match **only when the request already carries the
`csrf_token` cookie** (the tell that it's a browser riding an ambient credential). A
forged cross-site browser POST still carries the cookie and still fails without the
header; native clients bring no cookie and pass. Net-new `csrf_test.go` (the template had
zero CSRF coverage).

**Build B-emit — the manifest described field *types* but not the *contract*.** Four
classes of silent native-client failure the manifest couldn't have prevented:
`POST` body shapes were unknown; `kind:custom` endpoints were a black hole; a
`string`-typed `remind_at` was really a `datetime-local`; and child resources exposed no
relationship. Fixes: a lightweight `BodySchema{shape,model?,fields?}` on every endpoint
(derived per-kind for scaffolds, author-declared for `create_handler` via
`request_schema`/`response_schema`/`summary`); `format` hints via semantic DSL types
(`datetime`/`date`/`time`/`json`/`email`); FK `references` via a `ref:<model>` DSL type,
validated against existing models; generated create/update **echo the written object**;
and the app's read-only mirror extended so `/_manifest` serves it all instead of dropping
it. Auth endpoints deliberately excluded (their contract is fixed and hand-consumed).

**Build B-consume — iOS inferred instead of reading.** The deterministic
`export_manifest.py` now emits per-field `format`/`references`, per-endpoint schemas, a
Relationships section, and a Custom-endpoints section, and rewrites Screens-to-generate to
exclude children from top-level; `CLAUDE.md`/`build.md` teach `/build` to nest child
resources under the parent detail (filtered by FK), pick controls from `format`, and
render custom endpoints as actions. Custom endpoints were also removed from the legacy
Auth-endpoints section.

**Build C — no way to see the class of bug the tool most produces.** The app shipped
`testTargets: []`, so a navigation-binding bug (a `navigationDestination` that never
bound, so a row tapped into a blank and unwound) passed unit tests, the build, and every
grep. Fix: a generic pre-committed XCUITest that walks every tab and detects a real detail
push by the tapped **row becoming non-hittable** (deliberately *not* a nav-bar-button
check, which a list's `+` button would false-pass), plus a **required** per-detail-resource
assertion `/build` adds on top. Device-deploy defaults: ATS `NSAllowsLocalNetworking` and a
`DEVELOPMENT_TEAM` slot in `project.yml` (the durable source `xcodegen` regenerates from).

### Design principles this arc added

10. **The contract describes shapes, not just types.** A native client *sends* bodies, so
    the manifest must state request/response shapes — decoding types alone leaves the write
    path to inference.
11. **Semantic over storage.** A `string` that is a `datetime-local` must say so; a
    `format` hint distinguishes it from an RFC3339 timestamp and drives both the control and
    the wire format (no seconds, no `Z`).
12. **Declared-not-inferred extends to hand-written code.** Custom endpoints declare their
    own body + summary rather than being reverse-engineered from Go — the same discipline
    the scaffolds already had, applied where a human writes the handler.
13. **Relationships are data.** Foreign keys in the manifest drive nested navigation; a flat
    list per table is what you get when the relationship isn't published.
14. **Security defaults must not assume a browser.** CSRF gated on cookie presence and ATS
    local-networking are calibrated so a native client works out of the box while the browser
    path stays fully protected.
15. **Test at the layer the bug lives.** Navigation binding is invisible to unit tests and
    greps; only a UI test that taps the screen catches it. A generic best-effort smoke test
    plus *required* per-resource assertions — honest about the generic limitation — beats
    either alone.

### Follow-ups recorded this arc (non-blocking)

- `hmac.Equal("","")` empty-token CSRF bypass (pre-existing; one-line hardening).
- `scaffold_registration`'s `POST /register` carries no schema (auth-adjacent; iOS uses the
  pre-committed `AuthManager`).
- `parseFields` accepts a malformed `f:ref` (no target model) as a nonsense `ref` type.
- The generic iOS smoke test can miss a detail screen that itself contains cells — backstopped
  by the required per-resource assertions.
- Multi-FK children nest under *every* referenced parent (spec updated to match the emitter).
