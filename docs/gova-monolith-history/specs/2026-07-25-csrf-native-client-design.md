# Build A — CSRF applies only to browser double-submit participants

**Date:** 2026-07-25
**Scope:** `src/app/middleware/csrf.go` (+ new `csrf_test.go`). App-side only.
**Status:** Approved (design), pending plan.

## Problem

`middleware.CSRF` force-enrolls every client into the double-submit scheme. On a
mutating request (`POST`/`PUT`/`DELETE`) that carries no `csrf_token` cookie, the
current code *generates* a token, sets the cookie on the response, then compares
it against the `X-CSRF-Token` request header. A native client never sends that
header, so `hmac.Equal` fails and the request 403s.

The two existing exemptions do not cover the common case:

- `Authorization: Bearer …` — rescues an **authenticated mobile** client.
- path `== /api/v1/auth/login_token` — rescues **bearer login**.

A **no-auth native app** (an app built against a public, auth-free GOVA API)
sends neither, so every write it makes returns 403. This is a template-wide
correctness bug, not homelab-specific — it affects every no-auth app on mobile.

Field evidence: an iOS app translated from a no-auth monolith app had every
mutation 403 until the middleware was patched locally.

## Principle

Double-submit CSRF defends against a **browser auto-attaching an ambient
credential** to a forged cross-site request. The signal that a client *is* that
browser is that **it already carries the `csrf_token` cookie**, set during an
earlier safe-method page load. A client that brings no such cookie is not a
browser participating in the scheme — there is nothing for CSRF to protect.

**Rule:** enforce the header match only when the request actually carries the
`csrf_token` cookie. Otherwise pass through.

## Why this is safe (case analysis)

| Client | Brings `csrf_token` cookie? | Behavior | Safe? |
|---|---|---|---|
| Browser, authed or not, real user | Yes (GET set it) | header must match | ✅ unchanged |
| Browser, **forged cross-site POST** | Yes (auto-attached) but header can't match (attacker can't read cookie cross-origin) | **403, blocked** | ✅ no regression |
| Native, no-auth | No cookie jar | pass through | ✅ **bug fixed** |
| Native, bearer | n/a — `Bearer` fast-path | pass through | ✅ unchanged |
| Browser login POST | Yes (login page GET set it) | header must match | ✅ still protected |
| Bearer login (`login_token`) | No | `login_token` fast-path → pass | ✅ unchanged |

The one behavioral narrowing: a mutating request from a browser that has **never**
performed a safe-method load (so has no cookie yet) is no longer forced to 403 —
but such a request also has no session to ride, so there is nothing to protect.
Real browser flows always GET a page (which sets the cookie) before mutating.

## Change

In `CSRF`:

1. Read the **incoming** `csrf_token` cookie once, up front, recording whether it
   was present (`hasCookie`).
2. On a **safe** method with no cookie, generate a token and set the cookie (so
   browser JS can read it) — unchanged intent.
3. On a **mutating** method:
   - if `!hasCookie` → `next()` through (native / non-browser client).
   - if `hasCookie` → require `hmac.Equal(cookieValue, header)`, else 403.
4. Do **not** generate-and-set a cookie inside the mutating branch (the bug).
5. Keep the `Bearer` and `login_token` fast-path exemptions as documented
   defense-in-depth; with the new rule they are redundant-but-harmless (both
   bring no cookie), and the comments already explain them.

The context value (`csrf_token`) still carries whatever token the request had (or
a freshly generated one on safe methods) so `CSRFToken(r)` keeps working for the
login page.

## Test — `src/app/middleware/csrf_test.go` (net-new)

The template currently ships **no** CSRF test. Add one that mutation-proves the fix:

1. Mutating (`POST`) request with **no** `csrf_token` cookie → handler runs, 200.
   (Fails on the pre-fix code, which 403s.)
2. Mutating request **with** cookie + **wrong** `X-CSRF-Token` → 403.
3. Mutating request **with** cookie + **matching** `X-CSRF-Token` → 200.
4. Safe (`GET`) request with no cookie → 200 and response sets a `csrf_token`
   cookie (browser can read it).

Use `httptest` against `CSRF(next)` with a trivial 200 `next`.

## Non-goals

- No change to session auth, rate limiting, or the manifest.
- No client changes — the fix is purely server-side and needs no cooperation
  from any client.
- No mcp image rebuild — this is an app-side (`src/app`) file; `docker compose
  restart app` suffices. No `src/builder` template change.

## Verification

- `docker compose exec app go test ./middleware/...` (or `./...`) green.
- `docker compose restart app`, then live: an unauthenticated `POST` with no
  `csrf_token` cookie succeeds (was 403); a browser session with a bad token
  still 403s.
