# Build B-emit — Manifest carries the full request/response contract

**Date:** 2026-07-25
**Repo:** gova-monolith (emit side). Consumer side is a separate spec (`B-consume`, gova-ios).
**Status:** Approved in substance (design + forks), pending written-spec review.

## Why

`api.json` describes field *types* but not the *shapes a client sends and
receives*, nor the *semantics* of string-typed fields, nor *relationships*. A
native client translated from a real app hit four classes of silent failure the
contract could not have prevented:

1. It could not know a `POST /reminders` body, or what the endpoint returns.
2. `kind:"custom"` endpoints carried no body and no semantics at all — "a black hole."
3. `remind_at: string` is really a `datetime-local` value; the client rendered raw
   text and an RFC3339 writer would have corrupted the web editor.
4. `subtask` / `log_entry` are children of a parent row, but the manifest exposed no
   foreign-key relationship, so the natural output was 13 flat top-level lists.

This spec makes the monolith **emit** a complete contract. A companion spec makes
gova-ios **consume** it.

## Decisions (locked)

- **Body-schema representation:** a lightweight closed shape, not full JSON Schema
  (we control both ends; JSON Schema is overkill).
- **Semantic formats & relationships:** declared by extending the existing `fields`
  DSL (`remind_at:datetime`, `category_id:ref:log_category`) — a stated fact,
  validated against the real table/models, never name-guessed.
- **Echo fix bundled:** generated `create`/`update` handlers return the full written
  object, making `response:{object,model}` truthful and saving native clients a
  re-fetch.
- **Auth endpoints are out of scope for schemas.** Their contract is fixed and
  already hand-implemented in gova-ios's pre-committed `AuthManager`. `request`/
  `response` stay omitted for auth-kind endpoints. Schemas target the reviewer's
  actual pain: resource + custom endpoints.

## Manifest type changes (`src/builder/manifest.go`)

```go
type BodySchema struct {
	Shape  string       `json:"shape"`            // "object" | "list" | "empty"
	Model  string       `json:"model,omitempty"`  // fields inherited from this model
	Fields []ModelField `json:"fields,omitempty"` // inline fields / overrides
}

type ModelField struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	Format     string `json:"format,omitempty"`     // NEW: semantic hint
	References string `json:"references,omitempty"`  // NEW: FK target model
}

type Endpoint struct {
	Method   string      `json:"method"`
	Path     string      `json:"path"`
	Handler  string      `json:"handler"`
	Deps     []string    `json:"deps"`
	Auth     bool        `json:"auth"`
	Model    string      `json:"model,omitempty"`
	Kind     string      `json:"kind"`
	Summary  string      `json:"summary,omitempty"`   // NEW: human semantics (custom eps)
	Request  *BodySchema `json:"request,omitempty"`   // NEW
	Response *BodySchema `json:"response,omitempty"`  // NEW
}
```

`manifestHash` already marshals models+endpoints, so any schema/format/reference
change flows into the hash automatically — `_version`'s `manifest_hash` still
detects every surface change, now including body shapes.

## Body-schema derivation (scaffolded endpoints)

Let `writable(M)` = M's fields minus the implicit `id` and `created_at`. Let
`full(M)` mean `{shape:"object", model:M}` (all fields, echoed). Per kind:

| kind   | request                              | response                                   |
|--------|--------------------------------------|--------------------------------------------|
| list   | omitted                              | `{shape:"list",   model:M}`                 |
| detail | omitted                              | `{shape:"object", model:M}`                 |
| create | `{shape:"object", fields:writable(M)}` | `{shape:"object", model:M}` (echo)         |
| update | `{shape:"object", fields:writable(M)}` | `{shape:"object", model:M}` (echo)         |
| delete | omitted                              | `{shape:"object", fields:[{name:"ok",type:"boolean"}]}` |

`writable(M)` carries each field's `format`/`references`, so a create body knows
`remind_at` is a `datetime-local` and `category_id` references `log_category`.

## DSL extension (`src/builder/main.go`, `schema.go`)

`parseFields` gains two logical-type families. Storage is validated against the
real column via `applySchemaAt`/`expectedSQLType` (unchanged validation contract —
these all resolve to an existing affinity):

| DSL token                    | SQL affinity | manifest `type` | manifest extra                 | Go type | Swift |
|------------------------------|--------------|-----------------|--------------------------------|---------|-------|
| `f:datetime`                 | TEXT         | `string`        | `format:"datetime-local"`      | string  | String |
| `f:date`                     | TEXT         | `string`        | `format:"date"`                | string  | String |
| `f:time`                     | TEXT         | `string`        | `format:"time"`                | string  | String |
| `f:json`                     | TEXT         | `string`        | `format:"json"`                | string  | String |
| `f:email`                    | TEXT         | `string`        | `format:"email"`               | string  | String |
| `f:ref:<model>`              | INTEGER      | `int`           | `references:"<model>"`         | int64   | Int   |

- `Field` struct gains `Format string` and `Ref string`.
- `parseFields`: a token with 3 colon-parts (`name:ref:target`) sets `Type="ref"`,
  `Ref=target`. A token whose type is a semantic name sets `Type=<semantic>` (the
  format is derived at `fieldsToModel` time from a `semanticFormat` map).
- `expectedSQLType`: `ref` → `INTEGER`; the semantic string types fall through to
  the `TEXT` default (no change needed, but assert it in a test).
- `goType`/`goFieldType` funcMap + `_SWIFT`-parallel: `ref` → `int64`; semantic
  string types → `string`. So generated models compile and store correctly.
- `fieldsToModel`: maps semantic `Type` → manifest `{type:"string", format:...}`
  and `ref` → `{type:"int", references:Ref}`. `id`/`created_at` unchanged.
- **Validation:** a `ref:<model>` whose `<model>` is not already a model in the
  manifest fails the tool with a clear error (stated-fact discipline — no dangling
  references). This means the parent must be scaffolded before its child, which is
  the natural build order anyway.

## Echo fix (`src/builder/templates/resource_handlers.go.tmpl`, `model.go.tmpl`)

Generated `create` and `update` currently return `{id}` / `{ok}`. Change the
templates so both re-read the written row (the model already has `Find`) and return
the **full object** via `jsonOK(w, obj)`. The model's `Create` already returns the
new id; `Update` returns `sql.ErrNoRows` on a missing id (unchanged 404 path). No
model API change beyond ensuring a post-write `Find` is available (it is).

Update the generated resource-handler test to assert the create/update response
body contains the written fields, not just `id`.

## create_handler declares its own contract (`src/builder/main.go`)

`create_handler` gains three optional args:

- `request_schema` — a JSON string parsed into `BodySchema`; invalid JSON or an
  unknown `shape` fails the tool.
- `response_schema` — same.
- `summary` — a one-line human description of what the endpoint does.

They populate the custom endpoint's `Request`/`Response`/`Summary`. Omitting them
is allowed (the endpoint is then declared as today, shape-less) — but the tool
description will steer authors to supply them, since this is what makes a custom
endpoint legible to a native client. `create_page` is unchanged (GET page handler,
no body).

## Files touched

- `src/builder/manifest.go` — new types/fields; `fieldsToModel` mapping; derivation
  helpers for resource/auth endpoint schemas.
- `src/builder/main.go` — `Field` struct; `parseFields`; `goType`/`goFieldType`
  funcMap; `create_handler` args + wiring; call sites that build `resourceEndpoints`
  now attach derived schemas.
- `src/builder/schema.go` — `expectedSQLType` handles `ref`.
- `src/builder/templates/resource_handlers.go.tmpl` — create/update echo the object.
- `src/builder/templates/*_test.go.tmpl` — assert echo; assert format/ref survive.
- `src/builder/*_test.go` — manifest/schema/parseFields unit tests for the new shapes.

**mcp image rebuild required** (`docker compose up -d --build`) — templates and
generator are `go:embed`-ed at image-build time.

## Non-goals

- No iOS changes (separate `B-consume` spec).
- No auth-endpoint schemas (fixed, already consumed by pre-committed Swift).
- No filter operators beyond equality; no per-field 422 (still deferred).
- Not solving the pre-existing "model marshals password field to JSON" follow-up.

## Verification

- `go test ./...` in `src/builder` green (new unit tests for parseFields, schema,
  manifest derivation, echo).
- Rebuild mcp image; run a live scaffold of a parent+child with a `datetime` and a
  `ref` field; `curl /api/v1/_manifest` and confirm: derived request/response
  schemas present, `format:"datetime-local"` on the datetime field, `references` on
  the FK field, and a `create` returns the full written object. Then revert the
  scaffold to a clean tree.
- A `create_handler` with `request_schema`/`response_schema`/`summary` shows them in
  the manifest; malformed JSON fails the tool with a clear message.
