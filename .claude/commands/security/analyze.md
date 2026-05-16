---
description: Two-pass security audit on src/app/ — outputs .security/SECURITY_REPORT.md
---

You are running a security audit of the GOVA application source in `src/app/`.

---

## Pass 1: Go Reconnaissance — Entry Points

Read all files in `src/app/handlers/`. List every location where untrusted external data enters:

- `r.URL.Query().Get(...)` — URL query parameters
- JSON body fields decoded via `json.NewDecoder(r.Body).Decode(...)`
- `r.PathValue(...)` / `chi.URLParam(r, ...)` — path parameters
- `r.Header.Get(...)` — request headers
- Return values from model methods (data originally from user input)

Record each as: `file:line — source type — variable name`

---

## Pass 2: Go Investigation — Trace to Sinks

For each Go entry point, trace to output sinks:

| Threat | Sink Pattern | Verdict |
|---|---|---|
| **SQLi** | `db.Exec(fmt.Sprintf(..., userVar))` | CRITICAL |
| **SQLi** | `db.Query("... " + userVar)` | CRITICAL |
| **Path Traversal** | `os.Open(userVar)`, `http.ServeFile(w, r, userVar)` | HIGH |
| **CSRF** | POST handler without global CSRF middleware | HIGH |
| **Auth Bypass** | Handler returning sensitive data without `middleware.RequireAuth` or `middleware.UserID(r) != 0` | HIGH |
| **Command Injection** | `exec.Command(...)` with user input | CRITICAL |
| **Open Redirect** | `http.Redirect(w, r, userVar, ...)` without `strings.HasPrefix(userVar, "/")` | MEDIUM |
| **Hardcoded Secrets** | String literals matching API key patterns not from `os.Getenv(...)` | HIGH |

Note: XSS via `fmt.Fprintf(w, ...)` is NOT a concern — all handlers return `application/json` and `encoding/json` auto-escapes output.

---

## Pass 3: JS Audit

Read all files in `src/app/static/js/`. Check for:

| Threat | Pattern | Severity |
|---|---|---|
| **XSS** | `element.innerHTML = ` any variable | Critical |
| **XSS** | `document.write(` with any variable | Critical |
| **Code injection** | `eval(` with any external data | Critical |
| **Code injection** | `new Function(` with any external data | Critical |
| **Missing CSRF** | `fetch(` with POST/PUT/DELETE method without `X-CSRF-Token` header — check for raw `fetch()` bypassing `api.js` | High |
| **Auth bypass** | Protected page JS missing `requireAuth()` call at module init | High |
| **Data exposure** | `console.log(` with tokens, passwords, or session data | Medium |

---

## Output

Create `.security/` if it doesn't exist. Write findings to `.security/SECURITY_REPORT.md`:

```markdown
# Security Report — [date]

## Summary
- Critical: N
- High: N
- Medium: N

## Findings

### [CRITICAL] XSS in static/js/projects.js:42
**File:** `src/app/static/js/projects.js:42`
**Issue:** User-supplied `name` assigned to `innerHTML`
**Remediation:**
// Before:
el.innerHTML = item.name;
// After:
el.textContent = item.name;
```

Severity: Critical, High, Medium only. Omit Low. Include file, line, issue, and remediation for each finding.
