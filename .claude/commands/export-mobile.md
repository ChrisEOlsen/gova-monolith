---
description: Export web app context for mobile translation — generates the Generated Context block for a gova-ios or gova-android SEED.md
---

You are generating a mobile context export from this gova-monolith web app. Read this file completely before taking any action.

---

## Step 1: Read the current app state

Call the `inspect_app` MCP tool. It returns:
- Models (Go structs in models/)
- Handlers (files in handlers/)
- Pages (HTML files in static/pages/)
- JS modules (files in static/js/)
- Routes registered in main.go

Record every JS module listed (excluding files in static/js/lib/).

---

## Step 2: Read each JS module

For each JS module (not in lib/), read the file at `/src/app/static/js/<filename>.js` and extract:

**API calls** — find every call to `get(`, `post(`, `del(` from api.js:
- Pattern: `get('/api/...')` → GET endpoint
- Pattern: `post('/api/...')` → POST endpoint
- Pattern: `del('/api/...')` → DELETE endpoint

**Auth requirement** — check for `requireAuth()`:
- If present → `auth_required: true`
- If absent → `auth_required: false`

**Primary model** — infer from the GET endpoint path. E.g. `get('/api/items')` → model is `Item`.

---

## Step 3: Read model field types

For each model inferred in Step 2, read its Go file at `/src/app/models/<ModelName>.go`.

Extract field names and types from the struct definition. Map Go types to mobile types:
- `string` → `string`
- `int64` / `int` → `int`
- `bool` → `boolean`
- `float64` → `float`
- `time.Time` → `created_at` (always Date on mobile)

---

## Step 4: Check auth system

Read `/src/app/main.go`. Check whether these routes exist:
- `/api/auth/login` → cookie auth scaffolded
- `/api/auth/login_token` → mobile auth scaffolded

Note: mobile auth endpoints (`login_token`, `logout_token`, `me_token`) are added by `scaffold_mobile_auth`.
If they do not yet exist, note this in the export output so the developer knows to run `scaffold_mobile_auth` first.

---

## Step 5: Generate the context block

Write the following structured markdown. Fill in all values from Steps 2–4. Do not include placeholder text — every field must have a real value from the app.

```markdown
## Generated Context
> Auto-populated by /export:mobile. Do not edit manually.

### Screens
<!-- One entry per JS module (excluding lib/) -->
- <screen_name>:
  auth_required: <true|false>
  endpoints:
    - <METHOD> <path>   <!-- e.g. GET /api/items -->
    - <METHOD> <path>
  fields: <field1>:<type>, <field2>:<type>, ...   <!-- from Go model struct -->

### Auth
required: <true|false>
endpoints:
  - POST /api/auth/login_token
  - DELETE /api/auth/logout_token
  - GET /api/auth/me_token
note: <"mobile auth ready" if login_token route exists, else "run scaffold_mobile_auth first">

### API Base URL
(set by developer in SEED.md — default http://localhost:8080)
```

---

## Step 6: Save the output

Write the generated context block to a file called `mobile-seed-context.md` at the repo root (`/src/app/../../mobile-seed-context.md` → the monolith project root, same level as CLAUDE.md).

Use the exact path: write to the current working directory as `mobile-seed-context.md`.

---

## Step 7: Report to the developer

Tell the developer:

> **Mobile context exported to `mobile-seed-context.md`.**
>
> Next steps:
> 1. Open `mobile-seed-context.md` and review the generated context
> 2. In your `gova-ios` (or `gova-android`) repo, open `SEED.md`
> 3. Paste the entire contents of `mobile-seed-context.md` below the placeholder comment in the Generated Context section
> 4. Fill in the App Name and API Base URL at the top of SEED.md
> 5. Run `/build` in your mobile repo

If `scaffold_mobile_auth` has not been run yet (login_token route missing from main.go), also say:

> **Before running /build in your mobile repo:** run `scaffold_mobile_auth` here in gova-monolith
> and wire the 3 routes it prints into `main.go`. This adds the token auth endpoints the mobile app needs.
