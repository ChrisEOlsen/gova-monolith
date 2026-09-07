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

**Request bodies are capped at 1 MiB.** A larger body is a `413` (`validation_failed`);
a malformed one is a `400` (`validation_failed`). Both are the client's fault and both
are answered in the envelope.

**Every API response carries `Cache-Control: no-store`.** Responses are per-session
data, and the back button must not serve the previous user's `/auth/me` on a shared
machine.

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

Both are resolved by the same middleware, so **`auth: true` means the same thing to a
browser and to a native client**: send `Authorization: Bearer <token>` and every guarded
endpoint accepts it, not just the three `_token` ones.

`POST /api/v1/auth/logout_all` deletes the caller's bearer tokens as well as bumping the
session epoch. A native client is signed out by it and must log in again.

| Method | Path | Purpose |
|---|---|---|
| POST | `/api/v1/auth/login` | cookie login |
| POST | `/api/v1/auth/logout` | clear this browser's cookie |
| POST | `/api/v1/auth/logout_all` | retire every credential on every device — cookies **and** bearer tokens |
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

**`Strict-Transport-Security: max-age=63072000; includeSubDomains`** is sent on every
response. Browsers ignore it over plain HTTP, so there is no dev/prod branch.

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
      "name": "project", "table": "projects", "owned": true,
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
      "model": "project", "kind": "list", "auth": true,
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

**`auth`** — whether the route is wrapped in `RequireAuth`. Everything the scaffolder
emits is `true`; `gova <cmd> -public` is what produces `false`. A client should send its
credential on every request regardless — the flag describes the server's requirement, not
the client's obligation.

**`owned`** — present and `true` on a per-user resource. Every row the API returns for
that model belongs to the caller, and a row belonging to anyone else answers `404`. The
`user_id` column that makes this work is **not** in `fields`: like `id` and `created_at`
it is set by the server, so a client neither sends it nor receives it. A client needs no
special handling for an owned model beyond knowing that its lists are already scoped.

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
