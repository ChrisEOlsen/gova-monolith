# API Contract

What crosses the wire between this app and its clients — the browser modules in
`src/app/static/js/`, and the SwiftUI client in the companion `gova-ios` repo.

**Changing anything on this page breaks a client.** Everything not on this page is
internal and free to change.

---

## Response envelope

Every JSON endpoint answers with one shape:

```json
{ "ok": true,  "data": [ ... ], "meta": { "limit": 50, "offset": 0, "total": 123 } }
{ "ok": false, "error": "Name is required", "code": "validation_failed", "fields": { "name": "required" } }
```

- `error` is always a plain string. `code` and `fields` are additive.
- `meta` is present on list responses only.
- Unmatched paths and wrong methods under `/api/` answer in the envelope too.

`code` is a closed set — clients switch on it:

```
unauthorized  forbidden  not_found  method_not_allowed  conflict
validation_failed  rate_limited  unavailable  internal
```

Any 4xx that is not enumerated is `validation_failed`; anything else is `internal`.

## Data rules

**A list's `data` is never `null`.** An empty result is `[]`. A strict decoder binding
an array fails on `null`.

**Timestamps are RFC3339, UTC, second precision — no fractional part.** Swift's
`.iso8601` decoding strategy rejects fractional seconds, so `RFC3339Nano` decodes fine
in JavaScript and fails on iOS. This is why `models.Time` exists and why a model struct
never holds a bare `time.Time`. A `DATETIME` column is declared `timestamp`, never
`string`.

**Nullable columns marshal as JSON `null`** and are recorded `"nullable": true` in the
manifest, so a client knows to make the property optional.

## Pagination

Lists accept `?limit=` (1–200, default 50) and `?offset=`. Out-of-range values are
clamped, not rejected. Resource lists additionally accept `?sort=<[-]col>`
and `?filter=<col>:<value>`, both whitelisted against the model's real columns; an
unknown column is a 422.

## Paths

All API routes live under `/api/v1/`.

`GET /api/v1/_version` → `{ "api_version": "1.0.0", "min_client_version": "1.0.0" }`

The iOS client calls this at launch and blocks itself if its bundle version is below
`min_client_version`. It fails open on any error.

---

## Authentication

Auth ships with the template. There is no scaffolding step and no app without it.

**Browser — signed cookie.** `gova_session`, HMAC-SHA256, `HttpOnly`, `SameSite=Strict`,
`Secure` when `APP_ENV=production`.

**Native — bearer token.** 64-char hex, only its SHA-256 hash is stored.

| Method | Path | Purpose |
|---|---|---|
| POST | `/api/v1/auth/login` | cookie login |
| POST | `/api/v1/auth/logout` | clear this browser's cookie |
| POST | `/api/v1/auth/logout_all` | bump the session epoch — retires every cookie on every device |
| GET | `/api/v1/auth/me` | current user (cookie) |
| POST | `/api/v1/auth/register` | create an account, sets a session |
| POST | `/api/v1/auth/login_token` | bearer login |
| DELETE | `/api/v1/auth/logout_token` | revoke this bearer token |
| GET | `/api/v1/auth/me_token` | current user (bearer) |

Payloads:

```
POST /api/v1/auth/login        { "email", "password" } → { "id", "name", "email" }
POST /api/v1/auth/login_token  { "email", "password" } → { "token", "user": { "id", "name", "email" } }
GET  /api/v1/auth/me[_token]                          → { "id", "name", "email" }
```

A password hash never appears in any response.

**CSRF applies to cookie-authenticated browser requests only.** An unsafe method
carrying a session cookie must also send `X-CSRF-Token` matching the `csrf_token`
cookie; `api.js` does this automatically. A bearer request carries no ambient cookie
and is exempt.

**Rate limiting** is on by default: 5 attempts per 15 minutes per IP, and 20 per account,
on both login endpoints and on registration.

---

## The manifest — `src/app/api.json`

The committed source of truth for the served surface. The builder writes it; routes and
page routes are generated from it. It is also read directly off disk by gova-ios's
`export_manifest.py` — that repo does not call an HTTP endpoint.

Fields a client depends on:

```jsonc
{
  "api_version": "1.0.0",
  "models": [
    {
      "name": "project", "table": "projects",
      "fields": [
        { "name": "id",         "type": "int",       "nullable": false },
        { "name": "due_at",     "type": "string",    "nullable": true, "format": "datetime-local" },
        { "name": "client_id",  "type": "int",       "nullable": false, "references": "client" },
        { "name": "created_at", "type": "timestamp", "nullable": false }
      ]
    }
  ],
  "endpoints": [
    {
      "method": "GET", "path": "/api/v1/projects",
      "model": "project", "kind": "list", "auth": false,
      "summary": "...",                    // custom endpoints only
      "request":  { "shape": "object", "fields": [ ... ] },
      "response": { "shape": "list", "model": "project" }
    }
  ]
}
```

**`type`** — `string`, `int`, `float`, `boolean`, `timestamp`. An unrecognised type is
an error at scaffold time, not a silent `string`.

**`format`** — a semantic hint on a string-stored column, and the reason it exists is
that the iOS build picks a control from it:

| `format` | web input | SwiftUI control | value sent |
|---|---|---|---|
| `datetime-local` | `datetime-local` | `DatePicker([.date, .hourAndMinute])` | `2026-07-27T11:45` — no seconds, no `Z` |
| `date` | `date` | `DatePicker(.date)` | `2026-07-27` |
| `time` | `time` | `DatePicker(.hourAndMinute)` | `11:45` |
| `email` | `email` | `.keyboardType(.emailAddress)` | the string |
| `json` | `textarea` | monospaced `TextEditor` | the raw JSON string |

A `format: datetime-local` field is a `String`, not a `Date` — distinct from a
`timestamp` field, which is a `Date` and carries seconds and a zone.

**`references`** — names the parent model of a foreign key. A model with a `references`
field is a **child**: it gets no top-level screen, and its list renders inside the
parent's detail, loaded as `?filter=<fk>:<parentId>`.

**`kind`** — drives screen generation. The closed set:

```
list  detail  create  update  delete       resource CRUD
custom                                     a `gova handler` endpoint
auth_login  auth_logout  auth_logout_all  auth_me  register
mobile_login  mobile_logout  mobile_me
```

**`request` / `response`** — `{ "shape": "object" | "list" | "empty", "model"?, "fields"? }`.
Either `model` (fields inherited from that model) or `fields` (inline), never both. A
custom endpoint with request fields becomes a form; without them, a button.

### Not part of the contract

`pages`, `deps`, `handler`, `hash`, `generated_at`, `builder_version`. These are
internal to the web app's routing and provenance. No client may depend on them.
