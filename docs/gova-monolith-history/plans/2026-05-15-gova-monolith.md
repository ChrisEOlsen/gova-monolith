# GOVA Monolith Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a complete template repository (GOVA: Go + Vanilla JS + SQLite) that mirrors gotha-monolith's structure but replaces Templ/HTMX/Alpine with a pure JSON API + Vanilla ES modules frontend.

**Architecture:** Single Docker container, Go chi router serves JSON from `/api/*` and static files from `/static/*`. MCP builder server generates deterministic scaffolding. Vanilla ES modules handle all DOM rendering via `fetch()` + `createElement`. No frontend build step.

**Tech Stack:** Go 1.25, chi v5, go-sqlite3, golang.org/x/crypto, mcp-go, Tailwind CLI standalone, air (hot reload), Docker.

**No test suite** — verification steps use `docker compose logs`, `curl`, and MCP tool calls.

---

## File Map

```
gojs-monolith/
├── Dockerfile
├── docker-compose.yml
├── entrypoint.sh
├── .gitignore
├── env.example
├── SEED.md
├── CHECKLIST.md
├── README.md
├── CLAUDE.md
├── GEMINI.md
├── AGENTS.md
├── install-claude.sh
├── install-gemini.sh
├── install-opencode.sh
├── opencode.json
├── .claude/
│   ├── settings.local.json
│   └── commands/
│       ├── build.md
│       ├── launch.md
│       └── security/analyze.md
├── .gemini/
│   └── commands/
│       ├── build.toml
│       └── launch.toml
├── .opencode/
│   └── commands/
│       ├── build.md
│       └── launch.md
├── data/                        ← gitignored, SQLite lives here
├── logs/                        ← gitignored
└── src/
    ├── app/
    │   ├── .air.toml
    │   ├── go.mod
    │   ├── main.go
    │   ├── db/db.go
    │   ├── cache/cache.go
    │   ├── middleware/
    │   │   ├── auth.go          ← session cookie; RequireAuth returns JSON 401
    │   │   ├── csrf.go          ← validates X-CSRF-Token header only (not form field)
    │   │   └── security.go
    │   ├── handlers/
    │   │   ├── json.go          ← shared jsonOK / jsonError helpers
    │   │   └── home.go          ← serves static/pages/home.html
    │   ├── models/
    │   │   └── .gitkeep
    │   └── static/
    │       ├── pages/home.html
    │       ├── js/lib/
    │       │   ├── api.js
    │       │   └── auth.js
    │       └── css/
    │           ├── input.css
    │           └── style.css    ← gitignored, compiled by build_css
    └── builder/
        ├── go.mod
        ├── main.go              ← MCP server + all tool handlers
        └── templates/
            ├── model.go.tmpl
            ├── user_model.go.tmpl
            ├── handler.go.tmpl
            ├── list_handler.go.tmpl
            ├── auth_handler.go.tmpl
            ├── logout_handler.go.tmpl
            ├── register_handler.go.tmpl
            ├── page.html.tmpl
            ├── page.js.tmpl
            ├── list_page.html.tmpl
            ├── list_page.js.tmpl
            ├── login_page.html.tmpl
            ├── login.js.tmpl
            ├── register_page.html.tmpl
            └── register.js.tmpl
```

---

## Phase 1: Infrastructure & Go App Core

### Task 1: Repo Root Files

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.yml`
- Create: `entrypoint.sh`
- Create: `.gitignore`
- Create: `env.example`

- [ ] **Step 1: Write Dockerfile**

```dockerfile
FROM golang:1.25

RUN apt-get update && apt-get install -y --no-install-recommends gcc curl git && rm -rf /var/lib/apt/lists/*

# Tailwind CSS standalone binary
RUN ARCH=$(uname -m) && \
    if [ "$ARCH" = "aarch64" ]; then TW_ARCH="linux-arm64"; else TW_ARCH="linux-x64"; fi && \
    curl -sL "https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-${TW_ARCH}" \
        -o /usr/local/bin/tailwindcss \
    && chmod +x /usr/local/bin/tailwindcss

# air (hot reload)
RUN go install github.com/air-verse/air@latest

# Build MCP server binary
WORKDIR /src/builder
COPY src/builder/ ./
RUN go mod tidy
RUN CGO_ENABLED=1 go build -o /usr/local/bin/mcp-server .

# Pre-download app dependencies
WORKDIR /src/app
COPY src/app/ ./
RUN go mod tidy

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 8080
CMD ["/entrypoint.sh"]
```

- [ ] **Step 2: Write docker-compose.yml**

```yaml
name: ${APP_NAME:-my-gova-app}

services:
  app:
    build: .
    ports:
      - "${APP_PORT:-8080}:8080"
    volumes:
      - ./src:/src
      - ./data:/data
      - ./logs:/logs
    env_file: .env
```

- [ ] **Step 3: Write entrypoint.sh**

```sh
#!/bin/sh
set -e

cd /src/app
/usr/local/bin/tailwindcss -i ./static/css/input.css -o ./static/css/style.css --minify
exec air -c .air.toml
```

- [ ] **Step 4: Write .gitignore**

```
.env
.mcp.json
.claude/settings.local.json
.security/
/tmp/
*.db
*.db-shm
*.db-wal
src/app/static/css/style.css
logs/
opencode.json
docs/
.gemini/settings.json
data/
```

- [ ] **Step 5: Write env.example**

```
APP_NAME=my-gova-app
APP_PORT=8080

# Session signing key — change to 32+ random bytes before use
SESSION_SECRET=change-me-to-32-random-bytes-before-use

# Public URL of the app (required for webhooks, email links, OAuth callbacks)
APP_URL=

# Controls the Secure flag on session cookies — set to "production" when deployed over HTTPS
APP_ENV=local

# Cloudflare Tunnel (added by /launch)
TUNNEL_TOKEN=

# Optional integrations
STRIPE_SECRET_KEY=
STRIPE_WEBHOOK_SECRET=
OPENROUTER_API_KEY=

APP_TIMEZONE=UTC

# Log file path
LOG_PATH=/logs/app.log
```

- [ ] **Step 6: Create data and logs directories**

```bash
mkdir -p data logs
touch logs/.gitkeep data/.gitkeep
```

- [ ] **Step 7: Commit**

```bash
git add Dockerfile docker-compose.yml entrypoint.sh .gitignore env.example data/.gitkeep logs/.gitkeep
git commit -m "feat: add repo infrastructure (Docker, compose, env)"
```

---

### Task 2: Go App Module, DB, Cache

**Files:**
- Create: `src/app/go.mod`
- Create: `src/app/.air.toml`
- Create: `src/app/db/db.go`
- Create: `src/app/cache/cache.go`

- [ ] **Step 1: Write src/app/go.mod**

```
module gova/app

go 1.25

require (
	github.com/go-chi/chi/v5 v5.2.1
	github.com/mattn/go-sqlite3 v1.14.24
	golang.org/x/crypto v0.23.0
)
```

- [ ] **Step 2: Write src/app/.air.toml**

```toml
root = "/src/app"
tmp_dir = "/tmp/air"

[build]
  pre_cmd = ["/usr/local/bin/tailwindcss -i ./static/css/input.css -o ./static/css/style.css --minify"]
  cmd = "go build -o /tmp/air/server ."
  bin = "/tmp/air/server"
  include_dir = ["."]
  include_ext = ["go"]
  exclude_dir = ["vendor"]
  delay = 500
  poll = true
  poll_interval = 500

[log]
  time = true

[color]
  main = "magenta"
  watcher = "cyan"
  build = "yellow"
  runner = "green"
```

- [ ] **Step 3: Write src/app/db/db.go**

```go
package db

import (
	"database/sql"
	"fmt"
	"runtime"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	Write *sql.DB
	Read  *sql.DB
}

func (d *DB) Close() error {
	werr := d.Write.Close()
	rerr := d.Read.Close()
	if werr != nil {
		return werr
	}
	return rerr
}

func Open(path string) (*DB, error) {
	if path == "" {
		path = "/data/app.db"
	}
	dsn := fmt.Sprintf(
		"file:%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on&_synchronous=NORMAL",
		path,
	)

	writeDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)
	if err := writeDB.Ping(); err != nil {
		return nil, err
	}

	readDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		writeDB.Close()
		return nil, err
	}
	n := max(4, runtime.NumCPU())
	readDB.SetMaxOpenConns(n)
	readDB.SetMaxIdleConns(n)
	if err := readDB.Ping(); err != nil {
		writeDB.Close()
		return nil, err
	}

	return &DB{Write: writeDB, Read: readDB}, nil
}
```

- [ ] **Step 4: Write src/app/cache/cache.go**

```go
package cache

import (
	"strings"
	"sync"
	"time"
)

type entry struct {
	value     []byte
	expiresAt time.Time
}

type Cache struct {
	mu    sync.RWMutex
	items map[string]entry
}

func New() *Cache {
	c := &Cache{items: make(map[string]entry)}
	go c.janitor()
	return c
}

func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	e, ok := c.items[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.value, true
}

func (c *Cache) Set(key string, value []byte, ttl time.Duration) {
	c.mu.Lock()
	c.items[key] = entry{value: value, expiresAt: time.Now().Add(ttl)}
	c.mu.Unlock()
}

func (c *Cache) Bust(prefix string) {
	c.mu.Lock()
	for k := range c.items {
		if strings.HasPrefix(k, prefix) {
			delete(c.items, k)
		}
	}
	c.mu.Unlock()
}

func (c *Cache) janitor() {
	for range time.Tick(5 * time.Minute) {
		now := time.Now()
		c.mu.Lock()
		for k, e := range c.items {
			if now.After(e.expiresAt) {
				delete(c.items, k)
			}
		}
		c.mu.Unlock()
	}
}
```

- [ ] **Step 5: Create models gitkeep**

```bash
mkdir -p src/app/models && touch src/app/models/.gitkeep
```

- [ ] **Step 6: Commit**

```bash
git add src/app/go.mod src/app/.air.toml src/app/db/ src/app/cache/ src/app/models/
git commit -m "feat: add Go app module, db (WAL), and cache"
```

---

### Task 3: Middleware

**Files:**
- Create: `src/app/middleware/auth.go`
- Create: `src/app/middleware/csrf.go`
- Create: `src/app/middleware/security.go`

- [ ] **Step 1: Write src/app/middleware/auth.go**

Note: `RequireAuth` returns JSON 401 (not a redirect) because it protects API routes only. Page auth is handled client-side via `requireAuth()` in JS.

```go
package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

type ctxKey string

const userIDKey ctxKey = "user_id"

type sessionPayload struct {
	UserID    int64 `json:"uid"`
	ExpiresAt int64 `json:"exp"`
}

var (
	sessionKey    = []byte(os.Getenv("SESSION_SECRET"))
	secureCookies = os.Getenv("APP_ENV") == "production"
)

func SetSession(w http.ResponseWriter, userID int64, ttl time.Duration) {
	payload, _ := json.Marshal(sessionPayload{
		UserID:    userID,
		ExpiresAt: time.Now().Add(ttl).Unix(),
	})
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, sessionKey)
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{
		Name:     "gova_session",
		Value:    encoded + "|" + sig,
		Path:     "/",
		HttpOnly: true,
		Secure:   secureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(ttl.Seconds()),
	})
}

func ClearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:   "gova_session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}

func UserID(r *http.Request) int64 {
	v, _ := r.Context().Value(userIDKey).(int64)
	return v
}

func Auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("gova_session")
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		parts := strings.SplitN(cookie.Value, "|", 2)
		if len(parts) != 2 {
			next.ServeHTTP(w, r)
			return
		}
		encoded, sig := parts[0], parts[1]
		mac := hmac.New(sha256.New, sessionKey)
		mac.Write([]byte(encoded))
		expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(sig), []byte(expected)) {
			next.ServeHTTP(w, r)
			return
		}
		raw, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		var p sessionPayload
		if err := json.Unmarshal(raw, &p); err != nil || time.Now().Unix() > p.ExpiresAt {
			next.ServeHTTP(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey, p.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuth returns JSON 401 for unauthenticated API requests.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserID(r) == 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"ok":false,"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 2: Write src/app/middleware/csrf.go**

GOVA only accepts the CSRF token via `X-CSRF-Token` header (not form field) since all state-changing requests are JSON.

```go
package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

const csrfKey ctxKey = "csrf_token"

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func CSRFToken(r *http.Request) string {
	v, _ := r.Context().Value(csrfKey).(string)
	return v
}

func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if cookie, err := r.Cookie("csrf_token"); err == nil {
			token = cookie.Value
		} else {
			token = generateToken()
			http.SetCookie(w, &http.Cookie{
				Name:     "csrf_token",
				Value:    token,
				Path:     "/",
				HttpOnly: false,
				SameSite: http.SameSiteStrictMode,
			})
		}

		ctx := context.WithValue(r.Context(), csrfKey, token)

		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			headerToken := r.Header.Get("X-CSRF-Token")
			if !hmac.Equal([]byte(token), []byte(headerToken)) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"ok":false,"error":"invalid CSRF token"}`))
				return
			}
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
```

- [ ] **Step 3: Write src/app/middleware/security.go**

```go
package middleware

import "net/http"

func Security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 4: Commit**

```bash
git add src/app/middleware/
git commit -m "feat: add middleware (auth, csrf header-only, security headers)"
```

---

### Task 4: Handlers and main.go

**Files:**
- Create: `src/app/handlers/json.go`
- Create: `src/app/handlers/home.go`
- Create: `src/app/handlers/.gitkeep`
- Create: `src/app/main.go`

- [ ] **Step 1: Write src/app/handlers/json.go**

```go
package handlers

import (
	"encoding/json"
	"net/http"
)

type envelope struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

func jsonOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(envelope{OK: true, Data: data})
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(envelope{OK: false, Error: msg})
}
```

- [ ] **Step 2: Write src/app/handlers/home.go**

```go
package handlers

import (
	"net/http"
)

func HomeGET() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./static/pages/home.html")
	}
}
```

- [ ] **Step 3: Create handlers gitkeep**

```bash
touch src/app/handlers/.gitkeep
```

- [ ] **Step 4: Write src/app/main.go**

```go
package main

import (
	"io"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"gova/app/cache"
	"gova/app/db"
	"gova/app/handlers"
	"gova/app/middleware"
)

func main() {
	if logPath := os.Getenv("LOG_PATH"); logPath != "" {
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
			log.SetOutput(io.MultiWriter(os.Stdout, f))
		}
	}

	if secret := os.Getenv("SESSION_SECRET"); len(secret) < 32 {
		log.Fatal("SESSION_SECRET must be set and at least 32 characters")
	}

	database, err := db.Open(os.Getenv("DB_PATH"))
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer database.Close()

	appCache := cache.New()
	_ = appCache

	r := chi.NewRouter()
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(middleware.Security)
	r.Use(middleware.CSRF)
	r.Use(middleware.Auth)

	// Static files
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	// Pages
	r.Get("/", handlers.HomeGET())

	// Generated API routes registered here by MCP tools
	// Use database.Read for GET handlers, database.Write for POST handlers
	// Example:
	//   r.Post("/api/auth/login",  handlers.LoginPOST(database.Read, database.Write, appCache))
	//   r.Post("/api/auth/logout", handlers.LogoutPOST())
	//   r.Get("/api/auth/me",      handlers.MeGET(database.Read, database.Write, appCache))

	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("GOVA app listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
```

- [ ] **Step 5: Commit**

```bash
git add src/app/handlers/ src/app/main.go
git commit -m "feat: add handlers (json helpers, home) and main.go"
```

---

### Task 5: Frontend Base (Static Files)

**Files:**
- Create: `src/app/static/pages/home.html`
- Create: `src/app/static/js/lib/api.js`
- Create: `src/app/static/js/lib/auth.js`
- Create: `src/app/static/css/input.css`

- [ ] **Step 1: Write src/app/static/pages/home.html**

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Home</title>
  <link rel="stylesheet" href="/static/css/style.css">
</head>
<body class="bg-gray-50 text-gray-900 min-h-screen flex flex-col">
  <nav class="bg-white border-b border-gray-200 px-6 py-4 flex items-center justify-between">
    <a href="/static/pages/home.html" class="font-semibold text-sm tracking-tight">GOVA</a>
    <div class="flex gap-6 text-sm text-gray-500">
      <a href="/static/pages/home.html" class="hover:text-gray-900 transition-colors">Home</a>
    </div>
  </nav>
  <main class="flex-1 px-6 py-8 max-w-4xl mx-auto w-full">
    <div id="app">Loading...</div>
  </main>
  <footer class="border-t border-gray-100 px-6 py-4 text-xs text-gray-400">
    Built with GOVA
  </footer>
  <script type="module" src="/static/js/home.js"></script>
</body>
</html>
```

- [ ] **Step 2: Create src/app/static/js/home.js stub**

```js
const app = document.getElementById('app');

function render() {
  const h1 = document.createElement('h1');
  h1.className = 'text-2xl font-semibold tracking-tight';
  h1.textContent = 'Welcome';
  app.innerHTML = '';
  app.appendChild(h1);
}

render();
```

- [ ] **Step 3: Write src/app/static/js/lib/api.js**

```js
const csrf = () => document.cookie.match(/csrf_token=([^;]+)/)?.[1] ?? '';

export async function get(path) {
  const res = await fetch(path, { credentials: 'same-origin' });
  if (!res.ok && res.status !== 401) {
    return { ok: false, error: `HTTP ${res.status}` };
  }
  return res.json();
}

export async function post(path, body = {}) {
  const res = await fetch(path, {
    method: 'POST',
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': csrf(),
    },
    body: JSON.stringify(body),
  });
  return res.json();
}

export async function put(path, body = {}) {
  const res = await fetch(path, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: {
      'Content-Type': 'application/json',
      'X-CSRF-Token': csrf(),
    },
    body: JSON.stringify(body),
  });
  return res.json();
}

export async function del(path) {
  const res = await fetch(path, {
    method: 'DELETE',
    credentials: 'same-origin',
    headers: { 'X-CSRF-Token': csrf() },
  });
  return res.json();
}
```

- [ ] **Step 4: Write src/app/static/js/lib/auth.js**

```js
import { get } from './api.js';

export async function requireAuth() {
  const res = await get('/api/auth/me');
  if (!res.ok) {
    window.location.href = '/static/pages/login.html';
    return null;
  }
  return res.data;
}

export async function redirectIfAuthed() {
  const res = await get('/api/auth/me');
  if (res.ok) {
    window.location.href = '/static/pages/home.html';
  }
}
```

- [ ] **Step 5: Write src/app/static/css/input.css**

```css
@tailwind base;
@tailwind components;
@tailwind utilities;
```

- [ ] **Step 6: Create directory stubs**

```bash
mkdir -p src/app/static/js/lib src/app/static/pages src/app/static/css
```

- [ ] **Step 7: Commit**

```bash
git add src/app/static/
git commit -m "feat: add static frontend base (home page, api.js, auth.js)"
```

---

### Task 6: Docker Build Verification

- [ ] **Step 1: Copy env.example to .env and set values**

```bash
cp env.example .env
# Edit .env: set SESSION_SECRET to output of: openssl rand -hex 32
```

- [ ] **Step 2: Build and start container**

```bash
docker compose up -d --build
```

- [ ] **Step 3: Verify app starts and home page serves**

```bash
docker compose logs app
# Expected: "GOVA app listening on :8080"

curl -s http://localhost:8080/ | head -5
# Expected: <!DOCTYPE html> content from home.html

curl -s http://localhost:8080/static/js/lib/api.js | head -3
# Expected: const csrf = () => ...
```

- [ ] **Step 4: Commit**

No new files — just a verification checkpoint.

---

## Phase 2: MCP Builder Server

### Task 7: Builder Module and MCP Skeleton

**Files:**
- Create: `src/builder/go.mod`
- Create: `src/builder/main.go` (skeleton — tool handlers added in subsequent tasks)

- [ ] **Step 1: Write src/builder/go.mod**

```
module gova/builder

go 1.25

require (
	github.com/mark3labs/mcp-go v0.27.0
	github.com/mattn/go-sqlite3 v1.14.24
)
```

- [ ] **Step 2: Write src/builder/main.go — skeleton with helpers and main()**

```go
package main

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed templates/*
var templateFS embed.FS

var (
	tmplCache   = map[string]*template.Template{}
	tmplCacheMu sync.RWMutex
)

var funcMap = template.FuncMap{
	"toPascal": toPascal,
	"toPlural": toPlural,
	"titleCase": func(s string) string {
		s = strings.ReplaceAll(s, "_", " ")
		words := strings.Fields(s)
		for i, w := range words {
			if len(w) > 0 {
				words[i] = strings.ToUpper(w[:1]) + w[1:]
			}
		}
		return strings.Join(words, " ")
	},
	"goType": func(t string) string {
		switch t {
		case "int":
			return "int64"
		case "boolean":
			return "bool"
		case "float":
			return "float64"
		default:
			return "string"
		}
	},
	"joinNames": func(fields []Field) string {
		names := make([]string, len(fields))
		for i, f := range fields {
			names[i] = f.Name
		}
		return strings.Join(names, ", ")
	},
	"scanFields": func(fields []Field, prefix string) string {
		refs := make([]string, len(fields))
		for i, f := range fields {
			refs[i] = prefix + toPascal(f.Name)
		}
		return strings.Join(refs, ", ")
	},
	"placeholders": func(fields []Field) string {
		p := make([]string, len(fields))
		for i := range fields {
			p[i] = "?"
		}
		return strings.Join(p, ", ")
	},
	"createParams": func(fields []Field) string {
		params := make([]string, len(fields))
		for i, f := range fields {
			goT := "string"
			switch f.Type {
			case "int":
				goT = "int64"
			case "boolean":
				goT = "bool"
			case "float":
				goT = "float64"
			}
			params[i] = f.Name + " " + goT
		}
		return strings.Join(params, ", ")
	},
	"insertArgs": func(fields []Field) string {
		args := make([]string, len(fields))
		for i, f := range fields {
			if f.Type == "password" {
				args[i] = "string(hashed)"
			} else {
				args[i] = f.Name
			}
		}
		return strings.Join(args, ", ")
	},
}

func getTemplate(name string) (*template.Template, error) {
	tmplCacheMu.RLock()
	t, ok := tmplCache[name]
	tmplCacheMu.RUnlock()
	if ok {
		return t, nil
	}
	data, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		return nil, err
	}
	t, err = template.New(name).Funcs(funcMap).Parse(string(data))
	if err != nil {
		return nil, err
	}
	tmplCacheMu.Lock()
	tmplCache[name] = t
	tmplCacheMu.Unlock()
	return t, nil
}

var safeIdentRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

func isSafeIdent(s string) bool { return safeIdentRe.MatchString(s) }

func toPascal(snake string) string {
	parts := strings.Split(snake, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

func toPlural(s string) string {
	if strings.HasSuffix(s, "y") {
		return s[:len(s)-1] + "ies"
	}
	if strings.HasSuffix(s, "s") {
		return s + "es"
	}
	return s + "s"
}

type Field struct {
	Name string
	Type string
}

func parseFields(raw []string) []Field {
	fields := make([]Field, 0, len(raw))
	for _, f := range raw {
		parts := strings.SplitN(f, ":", 2)
		if len(parts) == 2 {
			fields = append(fields, Field{Name: parts[0], Type: parts[1]})
		} else {
			fields = append(fields, Field{Name: parts[0], Type: "string"})
		}
	}
	return fields
}

type TemplateData struct {
	Name         string
	PascalName   string
	PluralName   string
	Fields       []Field
	HasPassword  bool
	AuthRequired bool
	Method       string
	Title        string
	Filename     string
	APIEndpoint  string
	SubmitLabel  string
	FormName     string
}

func newData(name string, fields []Field) TemplateData {
	hasPw := false
	for _, f := range fields {
		if f.Type == "password" {
			hasPw = true
		}
	}
	return TemplateData{
		Name:        name,
		PascalName:  toPascal(name),
		PluralName:  toPlural(name),
		Fields:      fields,
		HasPassword: hasPw,
	}
}

func errResult(msg string) *mcp.CallToolResult {
	return mcp.NewToolResultError(msg)
}

func renderToFile(tmplName, outPath string, data TemplateData) error {
	tmpl, err := getTemplate(tmplName)
	if err != nil {
		return err
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return tmpl.Execute(f, data)
}

func renderToString(tmplName string, data TemplateData) (string, error) {
	tmpl, err := getTemplate(tmplName)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func rawFieldsToStrings(raw []interface{}) []string {
	s := make([]string, len(raw))
	for i, v := range raw {
		s[i], _ = v.(string)
	}
	return s
}

func runPatternChecks() string {
	bannedPatterns := []struct{ pattern, message string }{
		{`db\.Exec\(fmt\.Sprintf`, "SQL injection risk: use prepared statements"},
		{`db\.Query\(fmt\.Sprintf`, "SQL injection risk: use prepared statements"},
		{`\.innerHTML\s*=`, "XSS risk: use textContent or createElement instead of innerHTML"},
	}
	violations := []string{}
	goFiles, _ := filepath.Glob("/src/app/handlers/*.go")
	jsFiles, _ := filepath.Glob("/src/app/static/js/*.js")
	for _, file := range append(goFiles, jsFiles...) {
		content, _ := os.ReadFile(file)
		for _, bp := range bannedPatterns {
			re := regexp.MustCompile(bp.pattern)
			if re.Match(content) {
				violations = append(violations, "  "+filepath.Base(file)+": "+bp.message)
			}
		}
	}
	if len(violations) > 0 {
		return "Pattern check FAILED — fix before deploying:\n" + strings.Join(violations, "\n")
	}
	return "Pattern check passed."
}

func main() {
	s := server.NewMCPServer("gova-builder", "1.0.0",
		server.WithToolCapabilities(false),
	)

	s.AddTool(mcp.NewTool("inspect_app",
		mcp.WithDescription("Return current app state: all models, handlers, JS pages, and registered routes. Call BEFORE scaffolding to avoid duplicates."),
	), handleInspectApp)

	s.AddTool(mcp.NewTool("execute_sql",
		mcp.WithDescription("Execute SQL DDL or DML against /data/app.db. Use FIRST — tables must exist before models. Never write raw SQL inside handlers."),
		mcp.WithString("query", mcp.Required(), mcp.Description("SQL to execute")),
	), handleExecuteSQL)

	s.AddTool(mcp.NewTool("create_model",
		mcp.WithDescription("Generate models/Name.go with GetAll/Find/Create/Update/Delete and 5-min cache. Table must exist first (run execute_sql)."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Model name in snake_case")),
		mcp.WithArray("fields", mcp.Required(), mcp.Description("Fields as name:type")),
	), handleCreateModel)

	s.AddTool(mcp.NewTool("create_handler",
		mcp.WithDescription("Generate a single JSON handler stub in handlers/name.go. Implement the TODO logic. Wire route in main.go after."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Handler name in snake_case")),
		mcp.WithString("method", mcp.Required(), mcp.Description("HTTP method: GET, POST, PUT, DELETE")),
		mcp.WithBoolean("auth_required", mcp.Description("Inject auth guard — returns JSON 401 if unauthenticated")),
	), handleCreateHandler)

	s.AddTool(mcp.NewTool("create_page",
		mcp.WithDescription("Generate: static/pages/filename.html + static/js/filename.js + handlers/filename.go stub. After: add forms with add_js_form, wire route in main.go."),
		mcp.WithString("filename", mcp.Required(), mcp.Description("Page filename without extension")),
		mcp.WithString("title", mcp.Required(), mcp.Description("Page title")),
		mcp.WithBoolean("auth_required", mcp.Description("JS module calls requireAuth() on load")),
	), handleCreatePage)

	s.AddTool(mcp.NewTool("scaffold_list",
		mcp.WithDescription("Generate 4 files: model + JSON list handler + HTML shell + JS module. After: add forms with add_js_form, wire route in main.go, call build_css."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Resource name in snake_case")),
		mcp.WithArray("fields", mcp.Required(), mcp.Description("Fields as name:type")),
	), handleScaffoldList)

	s.AddTool(mcp.NewTool("scaffold_auth",
		mcp.WithDescription("Generate full auth system: users + rate_limits tables, User model, login/logout/me JSON handlers and HTML pages. Wire 5 routes in main.go (printed in output)."),
	), handleScaffoldAuth)

	s.AddTool(mcp.NewTool("scaffold_registration",
		mcp.WithDescription("Generate registration JSON handler + HTML page. Run after scaffold_auth. Wire 2 routes in main.go (printed in output)."),
	), handleScaffoldRegistration)

	s.AddTool(mcp.NewTool("add_js_form",
		mcp.WithDescription("Inject a creation form into an existing JS module at the // @inject-forms marker. The form uses api.js for submission. Requires: (1) JS file exists with the marker, (2) a POST handler exists at api_endpoint."),
		mcp.WithString("page", mcp.Required(), mcp.Description("Target page filename without extension")),
		mcp.WithString("api_endpoint", mcp.Required(), mcp.Description("API endpoint the form POSTs to")),
		mcp.WithArray("fields", mcp.Required(), mcp.Description("Fields as name:type")),
		mcp.WithString("title", mcp.Description("Optional form section title")),
		mcp.WithString("submit_label", mcp.Description("Submit button label (default: Submit)")),
	), handleAddJSForm)

	s.AddTool(mcp.NewTool("build_css",
		mcp.WithDescription("Compile Tailwind CSS: static/css/input.css → static/css/style.css. Call after editing HTML classes."),
		mcp.WithBoolean("minify", mcp.Description("Minify output")),
	), handleBuildCSS)

	s.AddTool(mcp.NewTool("run_linter",
		mcp.WithDescription("Run 'go vet ./...' and check handlers + JS files for raw SQL, innerHTML XSS patterns. Run after scaffolding to verify generated code."),
	), handleRunLinter)

	if err := server.ServeStdio(s); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 3: Commit skeleton**

```bash
git add src/builder/go.mod src/builder/main.go
git commit -m "feat: add MCP builder skeleton (helpers, tool registrations, main)"
```

---

### Task 8: execute_sql and inspect_app Tool Handlers

Add these functions to `src/builder/main.go`.

- [ ] **Step 1: Add handleExecuteSQL and handleInspectApp to src/builder/main.go**

```go
func handleExecuteSQL(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, _ := req.Params.Arguments["query"].(string)
	if query == "" {
		return errResult("query is required"), nil
	}
	db, err := sql.Open("sqlite3", "/data/app.db?_foreign_keys=on")
	if err != nil {
		return errResult(err.Error()), nil
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, query); err != nil {
		return errResult(err.Error()), nil
	}
	return mcp.NewToolResultText("SQL executed successfully"), nil
}

func handleInspectApp(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	listDir := func(pattern, label string) string {
		files, _ := filepath.Glob(pattern)
		names := make([]string, 0, len(files))
		for _, f := range files {
			base := filepath.Base(f)
			if base == ".gitkeep" {
				continue
			}
			names = append(names, "  "+base)
		}
		if len(names) == 0 {
			return label + "\n  (none)"
		}
		return label + "\n" + strings.Join(names, "\n")
	}

	sections := []string{
		listDir("/src/app/models/*.go", "Models:"),
		listDir("/src/app/handlers/*.go", "Handlers:"),
		listDir("/src/app/static/pages/*.html", "Pages (HTML):"),
		listDir("/src/app/static/js/*.js", "Pages (JS):"),
	}

	mainContent, err := os.ReadFile("/src/app/main.go")
	if err == nil {
		routeRe := regexp.MustCompile(`r\.(Get|Post|Put|Delete|Patch)\("([^"]+)"`)
		matches := routeRe.FindAllStringSubmatch(string(mainContent), -1)
		routes := make([]string, 0, len(matches))
		for _, m := range matches {
			routes = append(routes, "  "+m[1]+" "+m[2])
		}
		if len(routes) == 0 {
			sections = append(sections, "Routes (main.go):\n  (none registered)")
		} else {
			sections = append(sections, "Routes (main.go):\n"+strings.Join(routes, "\n"))
		}
	}

	return mcp.NewToolResultText(strings.Join(sections, "\n\n")), nil
}
```

- [ ] **Step 2: Commit**

```bash
git add src/builder/main.go
git commit -m "feat: add execute_sql and inspect_app tool handlers"
```

---

### Task 9: create_model Tool and Template

**Files:**
- Create: `src/builder/templates/model.go.tmpl`
- Modify: `src/builder/main.go` (add handleCreateModel)

- [ ] **Step 1: Write src/builder/templates/model.go.tmpl**

```
package models

import (
	"database/sql"
	"encoding/json"
	"time"
	"gova/app/cache"
	{{- if .HasPassword}}
	"golang.org/x/crypto/bcrypt"
	{{- end}}
)

type {{.PascalName}} struct {
	ID        int64     `json:"id"`
	{{- range .Fields}}
	{{toPascal .Name}} {{goType .Type}} `json:"{{.Name}}"`
	{{- end}}
	CreatedAt time.Time `json:"created_at"`
}

type {{.PascalName}}Model struct {
	readDB  *sql.DB
	writeDB *sql.DB
	cache   *cache.Cache
}

func New{{.PascalName}}Model(readDB, writeDB *sql.DB, c *cache.Cache) *{{.PascalName}}Model {
	return &{{.PascalName}}Model{readDB: readDB, writeDB: writeDB, cache: c}
}

func (m *{{.PascalName}}Model) GetAll() ([]{{.PascalName}}, error) {
	const cacheKey = "{{.PluralName}}:all"
	if hit, ok := m.cache.Get(cacheKey); ok {
		var items []{{.PascalName}}
		if err := json.Unmarshal(hit, &items); err == nil {
			return items, nil
		}
	}
	rows, err := m.readDB.Query("SELECT id, {{joinNames .Fields}}, created_at FROM {{.PluralName}} ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []{{.PascalName}}
	for rows.Next() {
		var item {{.PascalName}}
		if err := rows.Scan(&item.ID, {{scanFields .Fields "&item."}}, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if data, err := json.Marshal(items); err == nil {
		m.cache.Set(cacheKey, data, 5*time.Minute)
	}
	return items, nil
}

func (m *{{.PascalName}}Model) Find(id int64) (*{{.PascalName}}, error) {
	row := m.readDB.QueryRow("SELECT id, {{joinNames .Fields}}, created_at FROM {{.PluralName}} WHERE id = ?", id)
	var item {{.PascalName}}
	err := row.Scan(&item.ID, {{scanFields .Fields "&item."}}, &item.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (m *{{.PascalName}}Model) Create({{createParams .Fields}}) (int64, error) {
	{{- range .Fields}}{{if eq .Type "password"}}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil { return 0, err }
	{{end}}{{end}}
	res, err := m.writeDB.Exec(
		"INSERT INTO {{.PluralName}} ({{joinNames .Fields}}) VALUES ({{placeholders .Fields}})",
		{{insertArgs .Fields}},
	)
	if err != nil {
		return 0, err
	}
	m.cache.Bust("{{.PluralName}}:")
	return res.LastInsertId()
}

func (m *{{.PascalName}}Model) Delete(id int64) error {
	_, err := m.writeDB.Exec("DELETE FROM {{.PluralName}} WHERE id = ?", id)
	if err == nil {
		m.cache.Bust("{{.PluralName}}:")
	}
	return err
}
```

- [ ] **Step 2: Add handleCreateModel to src/builder/main.go**

```go
func handleCreateModel(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, _ := req.Params.Arguments["name"].(string)
	if !isSafeIdent(name) {
		return errResult("invalid model name: only alphanumeric and underscore allowed"), nil
	}
	rawFields, _ := req.Params.Arguments["fields"].([]interface{})
	fields := parseFields(rawFieldsToStrings(rawFields))
	data := newData(name, fields)

	outPath := "/src/app/models/" + toPascal(name) + ".go"
	if err := renderToFile("model.go.tmpl", outPath, data); err != nil {
		return errResult(err.Error()), nil
	}
	return mcp.NewToolResultText("Created: " + outPath), nil
}
```

- [ ] **Step 3: Commit**

```bash
git add src/builder/templates/model.go.tmpl src/builder/main.go
git commit -m "feat: add create_model tool and model.go.tmpl"
```

---

### Task 10: create_handler Tool and Template

**Files:**
- Create: `src/builder/templates/handler.go.tmpl`
- Modify: `src/builder/main.go`

- [ ] **Step 1: Write src/builder/templates/handler.go.tmpl**

```
package handlers

import (
	"database/sql"
	"net/http"
	"gova/app/cache"
	{{- if .AuthRequired}}
	"gova/app/middleware"
	{{- end}}
)

func {{.PascalName}}{{.Method}}(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		{{- if .AuthRequired}}
		if middleware.UserID(r) == 0 {
			jsonError(w, "unauthorized", 401)
			return
		}
		{{- end}}
		// TODO: implement handler logic
		// Use model methods — never write raw SQL here
		jsonOK(w, nil)
	}
}
```

- [ ] **Step 2: Add handleCreateHandler to src/builder/main.go**

```go
func handleCreateHandler(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, _ := req.Params.Arguments["name"].(string)
	method, _ := req.Params.Arguments["method"].(string)
	authRequired, _ := req.Params.Arguments["auth_required"].(bool)
	if !isSafeIdent(name) {
		return errResult("invalid handler name"), nil
	}
	data := newData(name, nil)
	data.Method = strings.ToUpper(method)
	data.AuthRequired = authRequired

	outPath := "/src/app/handlers/" + name + ".go"
	if err := renderToFile("handler.go.tmpl", outPath, data); err != nil {
		return errResult(err.Error()), nil
	}
	return mcp.NewToolResultText("Created: " + outPath + "\n\nImplement the TODO logic. Wire route in main.go.\n\n" + runPatternChecks()), nil
}
```

- [ ] **Step 3: Commit**

```bash
git add src/builder/templates/handler.go.tmpl src/builder/main.go
git commit -m "feat: add create_handler tool and handler.go.tmpl"
```

---

### Task 11: create_page Tool and Templates

**Files:**
- Create: `src/builder/templates/page.html.tmpl`
- Create: `src/builder/templates/page.js.tmpl`
- Modify: `src/builder/main.go`

- [ ] **Step 1: Write src/builder/templates/page.html.tmpl**

```
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Title}}</title>
  <link rel="stylesheet" href="/static/css/style.css">
</head>
<body class="bg-gray-50 text-gray-900 min-h-screen flex flex-col">
  <nav class="bg-white border-b border-gray-200 px-6 py-4 flex items-center justify-between">
    <a href="/static/pages/home.html" class="font-semibold text-sm tracking-tight">GOVA</a>
    <div class="flex gap-6 text-sm text-gray-500">
      <a href="/static/pages/home.html" class="hover:text-gray-900 transition-colors">Home</a>
    </div>
  </nav>
  <main class="flex-1 px-6 py-8 max-w-4xl mx-auto w-full">
    <div class="space-y-6">
      <h1 class="text-2xl font-semibold tracking-tight">{{.Title}}</h1>
      <div id="app">Loading...</div>
      <div id="forms-container" class="space-y-4"></div>
    </div>
  </main>
  <footer class="border-t border-gray-100 px-6 py-4 text-xs text-gray-400">
    Built with GOVA
  </footer>
  <script type="module" src="/static/js/{{.Name}}.js"></script>
</body>
</html>
```

- [ ] **Step 2: Write src/builder/templates/page.js.tmpl**

```
import { get, post, del } from '/static/js/lib/api.js';
{{- if .AuthRequired}}
import { requireAuth } from '/static/js/lib/auth.js';
{{- end}}

const app = document.getElementById('app');

async function init() {
  {{- if .AuthRequired}}
  const user = await requireAuth();
  if (!user) return;
  {{- end}}
  render();
}

function render() {
  const p = document.createElement('p');
  p.className = 'text-sm text-gray-500';
  p.textContent = 'TODO: implement {{.Title}} page.';
  app.innerHTML = '';
  app.appendChild(p);
}

// @inject-forms

init();
```

- [ ] **Step 3: Add handleCreatePage to src/builder/main.go**

```go
func handleCreatePage(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	filename, _ := req.Params.Arguments["filename"].(string)
	title, _ := req.Params.Arguments["title"].(string)
	authRequired, _ := req.Params.Arguments["auth_required"].(bool)
	if !isSafeIdent(filename) {
		return errResult("invalid filename"), nil
	}
	data := newData(filename, nil)
	data.Title = title
	data.AuthRequired = authRequired
	data.Method = "GET"

	htmlPath := "/src/app/static/pages/" + filename + ".html"
	if err := renderToFile("page.html.tmpl", htmlPath, data); err != nil {
		return errResult(err.Error()), nil
	}
	jsPath := "/src/app/static/js/" + filename + ".js"
	if err := renderToFile("page.js.tmpl", jsPath, data); err != nil {
		return errResult(err.Error()), nil
	}
	handlerPath := "/src/app/handlers/" + filename + ".go"
	if err := renderToFile("handler.go.tmpl", handlerPath, data); err != nil {
		return errResult(err.Error()), nil
	}
	return mcp.NewToolResultText(
		"Created: " + htmlPath + "\nCreated: " + jsPath + "\nCreated: " + handlerPath +
			"\n\nNext: wire route in main.go. Add forms with add_js_form.\n\n" + runPatternChecks(),
	), nil
}
```

- [ ] **Step 4: Commit**

```bash
git add src/builder/templates/page.html.tmpl src/builder/templates/page.js.tmpl src/builder/main.go
git commit -m "feat: add create_page tool with HTML shell and JS module templates"
```

---

### Task 12: scaffold_list Tool and Templates

**Files:**
- Create: `src/builder/templates/list_handler.go.tmpl`
- Create: `src/builder/templates/list_page.html.tmpl`
- Create: `src/builder/templates/list_page.js.tmpl`
- Modify: `src/builder/main.go`

- [ ] **Step 1: Write src/builder/templates/list_handler.go.tmpl**

```
package handlers

import (
	"database/sql"
	"net/http"
	"gova/app/cache"
	"gova/app/models"
)

func {{.PascalName}}ListGET(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		model := models.New{{.PascalName}}Model(readDB, writeDB, appCache)
		items, err := model.GetAll()
		if err != nil {
			jsonError(w, "failed to load", 500)
			return
		}
		jsonOK(w, items)
	}
}
```

- [ ] **Step 2: Write src/builder/templates/list_page.html.tmpl**

```
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Title}}</title>
  <link rel="stylesheet" href="/static/css/style.css">
</head>
<body class="bg-gray-50 text-gray-900 min-h-screen flex flex-col">
  <nav class="bg-white border-b border-gray-200 px-6 py-4 flex items-center justify-between">
    <a href="/static/pages/home.html" class="font-semibold text-sm tracking-tight">GOVA</a>
    <div class="flex gap-6 text-sm text-gray-500">
      <a href="/static/pages/home.html" class="hover:text-gray-900 transition-colors">Home</a>
      <a href="/static/pages/{{.PluralName}}.html" class="hover:text-gray-900 transition-colors">{{.Title}}</a>
    </div>
  </nav>
  <main class="flex-1 px-6 py-8 max-w-4xl mx-auto w-full">
    <div class="space-y-6">
      <div class="flex items-center justify-between">
        <h1 class="text-2xl font-semibold tracking-tight">{{.Title}}</h1>
      </div>
      <section id="{{.Name}}-list" class="space-y-1">
        <p class="text-sm text-gray-500">Loading...</p>
      </section>
      <div id="forms-container" class="space-y-4"></div>
    </div>
  </main>
  <footer class="border-t border-gray-100 px-6 py-4 text-xs text-gray-400">
    Built with GOVA
  </footer>
  <script type="module" src="/static/js/{{.PluralName}}.js"></script>
</body>
</html>
```

- [ ] **Step 3: Write src/builder/templates/list_page.js.tmpl**

```
import { get, del } from '/static/js/lib/api.js';
{{- if .AuthRequired}}
import { requireAuth } from '/static/js/lib/auth.js';
{{- end}}

const listEl = document.getElementById('{{.Name}}-list');

export async function loadList() {
  const res = await get('/api/{{.PluralName}}');
  if (!res.ok) {
    listEl.textContent = 'Failed to load.';
    return;
  }
  renderList(res.data ?? []);
}

function renderList(items) {
  listEl.innerHTML = '';
  if (!items.length) {
    const p = document.createElement('p');
    p.className = 'text-sm text-gray-500';
    p.textContent = 'No items yet.';
    listEl.appendChild(p);
    return;
  }
  const ul = document.createElement('ul');
  ul.className = 'divide-y divide-gray-100';
  items.forEach(item => {
    const li = document.createElement('li');
    li.className = 'py-3 flex items-center justify-between';
    const nameSpan = document.createElement('span');
    nameSpan.className = 'text-sm font-medium';
    nameSpan.textContent = item.{{index .Fields 0|printf "%s"}};
    const dateSpan = document.createElement('span');
    dateSpan.className = 'text-xs text-gray-500';
    dateSpan.textContent = new Date(item.created_at).toLocaleDateString();
    li.appendChild(nameSpan);
    li.appendChild(dateSpan);
    ul.appendChild(li);
  });
  listEl.appendChild(ul);
}

// @inject-forms

async function init() {
  {{- if .AuthRequired}}
  const user = await requireAuth();
  if (!user) return;
  {{- end}}
  await loadList();
}

init();
```

Note: `item.{{index .Fields 0|printf "%s"}}` renders the first field name. In the Go template this is `{{(index .Fields 0).Name}}`.

- [ ] **Step 4: Fix list_page.js.tmpl field name rendering**

Replace the `nameSpan.textContent` line so the template renders the first field name correctly:

```
    nameSpan.textContent = item.{{"{{"}}(index .Fields 0).Name{{"}}"}};
```

Since this is a Go template generating JS, write the template file with proper Go template syntax. The `nameSpan.textContent` line should be:

```
    nameSpan.textContent = item.{{ "{{" }}(index .Fields 0).Name{{ "}}" }};
```

Write `list_page.js.tmpl` with this content (the `{{(index .Fields 0).Name}}` is Go template syntax that will be evaluated when the MCP tool runs):

```
import { get, del } from '/static/js/lib/api.js';
{{if .AuthRequired -}}
import { requireAuth } from '/static/js/lib/auth.js';
{{- end}}

const listEl = document.getElementById('{{.Name}}-list');

export async function loadList() {
  const res = await get('/api/{{.PluralName}}');
  if (!res.ok) {
    listEl.textContent = 'Failed to load.';
    return;
  }
  renderList(res.data ?? []);
}

function renderList(items) {
  listEl.innerHTML = '';
  if (!items.length) {
    const p = document.createElement('p');
    p.className = 'text-sm text-gray-500';
    p.textContent = 'No items yet.';
    listEl.appendChild(p);
    return;
  }
  const ul = document.createElement('ul');
  ul.className = 'divide-y divide-gray-100';
  items.forEach(item => {
    const li = document.createElement('li');
    li.className = 'py-3 flex items-center justify-between';
    const nameSpan = document.createElement('span');
    nameSpan.className = 'text-sm font-medium';
    nameSpan.textContent = item.{{`{{(index .Fields 0).Name}}`}};
    const dateSpan = document.createElement('span');
    dateSpan.className = 'text-xs text-gray-500';
    dateSpan.textContent = new Date(item.created_at).toLocaleDateString();
    li.appendChild(nameSpan);
    li.appendChild(dateSpan);
    ul.appendChild(li);
  });
  listEl.appendChild(ul);
}

// @inject-forms

async function init() {
  {{if .AuthRequired -}}
  const user = await requireAuth();
  if (!user) return;
  {{- end}}
  await loadList();
}

init();
```

**Important:** The `{{(index .Fields 0).Name}}` inside backtick-escaped `{{` `}}` is a Go template instruction — the MCP builder will replace it with the actual first field name when generating a JS file.

- [ ] **Step 5: Add handleScaffoldList to src/builder/main.go**

```go
func handleScaffoldList(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, _ := req.Params.Arguments["name"].(string)
	rawFields, _ := req.Params.Arguments["fields"].([]interface{})
	if !isSafeIdent(name) {
		return errResult("invalid name"), nil
	}
	fields := parseFields(rawFieldsToStrings(rawFields))
	if len(fields) == 0 {
		return errResult("at least one field is required"), nil
	}
	data := newData(name, fields)
	data.Title = toPascal(toPlural(name))

	type fileSpec struct{ tmpl, out string }
	specs := []fileSpec{
		{"model.go.tmpl", "/src/app/models/" + toPascal(name) + ".go"},
		{"list_handler.go.tmpl", "/src/app/handlers/" + name + "_list.go"},
		{"list_page.html.tmpl", "/src/app/static/pages/" + toPlural(name) + ".html"},
		{"list_page.js.tmpl", "/src/app/static/js/" + toPlural(name) + ".js"},
	}

	results := []string{}
	for _, spec := range specs {
		if err := renderToFile(spec.tmpl, spec.out, data); err != nil {
			return errResult(err.Error()), nil
		}
		results = append(results, "Created: "+spec.out)
	}
	return mcp.NewToolResultText(
		strings.Join(results, "\n") +
			"\n\nNext: wire GET route in main.go, add POST handler with create_handler, add form with add_js_form, call build_css.\n\n" +
			runPatternChecks(),
	), nil
}
```

- [ ] **Step 6: Commit**

```bash
git add src/builder/templates/list_handler.go.tmpl src/builder/templates/list_page.html.tmpl src/builder/templates/list_page.js.tmpl src/builder/main.go
git commit -m "feat: add scaffold_list tool and list templates"
```

---

### Task 13: scaffold_auth Tool and Templates

**Files:**
- Create: `src/builder/templates/user_model.go.tmpl`
- Create: `src/builder/templates/auth_handler.go.tmpl`
- Create: `src/builder/templates/logout_handler.go.tmpl`
- Create: `src/builder/templates/login_page.html.tmpl`
- Create: `src/builder/templates/login.js.tmpl`
- Modify: `src/builder/main.go`

- [ ] **Step 1: Write src/builder/templates/user_model.go.tmpl**

```
package models

import (
	"database/sql"
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gova/app/cache"
)

type User struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type UserModel struct {
	readDB  *sql.DB
	writeDB *sql.DB
	cache   *cache.Cache
}

func NewUserModel(readDB, writeDB *sql.DB, c *cache.Cache) *UserModel {
	return &UserModel{readDB: readDB, writeDB: writeDB, cache: c}
}

func (m *UserModel) Create(name, email, password string) (int64, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return 0, err
	}
	res, err := m.writeDB.Exec(
		"INSERT INTO users (name, email, password_hash) VALUES (?, ?, ?)",
		name, email, string(hashed),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (m *UserModel) FindByEmail(email string) (*User, error) {
	row := m.readDB.QueryRow(
		"SELECT id, name, email, password_hash, created_at FROM users WHERE email = ? LIMIT 1",
		email,
	)
	var u User
	if err := row.Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("user not found")
		}
		return nil, err
	}
	return &u, nil
}

func (m *UserModel) FindByID(id int64) (*User, error) {
	row := m.readDB.QueryRow(
		"SELECT id, name, email, created_at FROM users WHERE id = ? LIMIT 1",
		id,
	)
	var u User
	if err := row.Scan(&u.ID, &u.Name, &u.Email, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("user not found")
		}
		return nil, err
	}
	return &u, nil
}

func (m *UserModel) IsRateLimited(ip string) (bool, error) {
	var lockedUntil sql.NullTime
	row := m.readDB.QueryRow("SELECT locked_until FROM rate_limits WHERE ip = ?", ip)
	if err := row.Scan(&lockedUntil); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if lockedUntil.Valid && time.Now().Before(lockedUntil.Time) {
		return true, nil
	}
	return false, nil
}

func (m *UserModel) RecordFailedAttempt(ip string) {
	_, _ = m.writeDB.Exec(`
		INSERT INTO rate_limits (ip, attempts, locked_until, updated_at)
		VALUES (?, 1, NULL, CURRENT_TIMESTAMP)
		ON CONFLICT(ip) DO UPDATE SET
			attempts = attempts + 1,
			locked_until = CASE WHEN attempts + 1 >= 5
				THEN datetime('now', '+15 minutes') ELSE locked_until END,
			updated_at = CURRENT_TIMESTAMP
	`, ip)
}

func (m *UserModel) ClearAttempts(ip string) {
	_, _ = m.writeDB.Exec("DELETE FROM rate_limits WHERE ip = ?", ip)
}
```

- [ ] **Step 2: Write src/builder/templates/auth_handler.go.tmpl**

```
package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gova/app/cache"
	"gova/app/middleware"
	"gova/app/models"
)

func LoginPOST(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		email := strings.TrimSpace(body.Email)
		password := body.Password
		ip := r.RemoteAddr
		if i := strings.LastIndex(ip, ":"); i > 0 {
			ip = ip[:i]
		}

		userModel := models.NewUserModel(readDB, writeDB, appCache)

		if locked, err := userModel.IsRateLimited(ip); err != nil || locked {
			jsonError(w, "Too many attempts. Try again in 15 minutes.", http.StatusTooManyRequests)
			return
		}

		user, err := userModel.FindByEmail(email)
		if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
			userModel.RecordFailedAttempt(ip)
			jsonError(w, "Invalid email or password.", http.StatusUnauthorized)
			return
		}

		userModel.ClearAttempts(ip)
		middleware.SetSession(w, user.ID, 24*time.Hour)
		jsonOK(w, map[string]any{"id": user.ID, "name": user.Name, "email": user.Email})
	}
}

func MeGET(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := middleware.UserID(r)
		if uid == 0 {
			jsonError(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		userModel := models.NewUserModel(readDB, writeDB, appCache)
		user, err := userModel.FindByID(uid)
		if err != nil {
			jsonError(w, "user not found", http.StatusNotFound)
			return
		}
		jsonOK(w, map[string]any{"id": user.ID, "name": user.Name, "email": user.Email})
	}
}
```

- [ ] **Step 3: Write src/builder/templates/logout_handler.go.tmpl**

```
package handlers

import (
	"net/http"
	"gova/app/middleware"
)

func LogoutPOST() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		middleware.ClearSession(w)
		jsonOK(w, nil)
	}
}
```

- [ ] **Step 4: Write src/builder/templates/login_page.html.tmpl**

```
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Sign In</title>
  <link rel="stylesheet" href="/static/css/style.css">
</head>
<body class="bg-gray-50 text-gray-900 min-h-screen flex items-center justify-center">
  <div class="w-full max-w-sm space-y-6">
    <div class="text-center">
      <h1 class="text-2xl font-semibold tracking-tight">Sign in</h1>
      <p class="text-sm text-gray-500 mt-1">Enter your credentials to continue</p>
    </div>
    <div class="bg-white border border-gray-200 rounded-lg p-6 space-y-4">
      <div id="error-msg" class="hidden border border-red-200 bg-red-50 text-red-700 text-sm px-4 py-3 rounded"></div>
      <form id="login-form" class="space-y-4">
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Email</label>
          <input type="email" id="email" name="email" required
            class="block w-full border border-gray-300 rounded px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-gray-900">
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Password</label>
          <input type="password" id="password" name="password" required
            class="block w-full border border-gray-300 rounded px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-gray-900">
        </div>
        <button type="submit"
          class="w-full px-4 py-2 bg-gray-900 text-white text-sm rounded hover:bg-gray-700 transition-colors">
          Sign in
        </button>
      </form>
    </div>
  </div>
  <script type="module" src="/static/js/login.js"></script>
</body>
</html>
```

- [ ] **Step 5: Write src/builder/templates/login.js.tmpl**

```
import { post } from '/static/js/lib/api.js';
import { redirectIfAuthed } from '/static/js/lib/auth.js';

const form = document.getElementById('login-form');
const errorMsg = document.getElementById('error-msg');

async function init() {
  await redirectIfAuthed();

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    errorMsg.classList.add('hidden');
    const email = document.getElementById('email').value;
    const password = document.getElementById('password').value;

    const res = await post('/api/auth/login', { email, password });
    if (res.ok) {
      window.location.href = '/static/pages/home.html';
    } else {
      errorMsg.textContent = res.error ?? 'Login failed.';
      errorMsg.classList.remove('hidden');
    }
  });
}

init();
```

- [ ] **Step 6: Add handleScaffoldAuth to src/builder/main.go**

```go
func handleScaffoldAuth(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	db, err := sql.Open("sqlite3", "/data/app.db?_foreign_keys=on")
	if err != nil {
		return errResult(err.Error()), nil
	}
	defer db.Close()

	ddl := `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS rate_limits (
	ip TEXT NOT NULL,
	attempts INTEGER DEFAULT 0,
	locked_until DATETIME,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (ip)
);`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return errResult(err.Error()), nil
	}

	results := []string{"Created tables: users, rate_limits"}
	data := newData("user", nil)

	type fileSpec struct{ tmpl, out string }
	specs := []fileSpec{
		{"user_model.go.tmpl", "/src/app/models/User.go"},
		{"auth_handler.go.tmpl", "/src/app/handlers/auth.go"},
		{"logout_handler.go.tmpl", "/src/app/handlers/logout.go"},
		{"login_page.html.tmpl", "/src/app/static/pages/login.html"},
		{"login.js.tmpl", "/src/app/static/js/login.js"},
	}
	for _, spec := range specs {
		if err := renderToFile(spec.tmpl, spec.out, data); err != nil {
			return errResult(err.Error()), nil
		}
		results = append(results, "Created: "+spec.out)
	}
	results = append(results, "\nRegister routes in main.go:\n"+
		"  r.Post(\"/api/auth/login\",  handlers.LoginPOST(database.Read, database.Write, appCache))\n"+
		"  r.Post(\"/api/auth/logout\", handlers.LogoutPOST())\n"+
		"  r.Get(\"/api/auth/me\",      handlers.MeGET(database.Read, database.Write, appCache))")

	return mcp.NewToolResultText(strings.Join(results, "\n") + "\n\n" + runPatternChecks()), nil
}
```

- [ ] **Step 7: Commit**

```bash
git add src/builder/templates/user_model.go.tmpl src/builder/templates/auth_handler.go.tmpl src/builder/templates/logout_handler.go.tmpl src/builder/templates/login_page.html.tmpl src/builder/templates/login.js.tmpl src/builder/main.go
git commit -m "feat: add scaffold_auth tool and auth templates (JSON responses)"
```

---

### Task 14: scaffold_registration Tool and Templates

**Files:**
- Create: `src/builder/templates/register_handler.go.tmpl`
- Create: `src/builder/templates/register_page.html.tmpl`
- Create: `src/builder/templates/register.js.tmpl`
- Modify: `src/builder/main.go`

- [ ] **Step 1: Write src/builder/templates/register_handler.go.tmpl**

```
package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"gova/app/cache"
	"gova/app/middleware"
	"gova/app/models"
)

func RegisterPOST(readDB, writeDB *sql.DB, appCache *cache.Cache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name     string `json:"name"`
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			jsonError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(body.Name)
		email := strings.TrimSpace(body.Email)
		password := body.Password

		if name == "" || email == "" || len(password) < 8 {
			jsonError(w, "Name, email and password (min 8 chars) are required.", http.StatusBadRequest)
			return
		}

		userModel := models.NewUserModel(readDB, writeDB, appCache)
		id, err := userModel.Create(name, email, password)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				jsonError(w, "An account with that email already exists.", http.StatusConflict)
				return
			}
			jsonError(w, "Registration failed. Please try again.", http.StatusInternalServerError)
			return
		}

		middleware.SetSession(w, id, 24*time.Hour)
		jsonOK(w, map[string]any{"id": id})
	}
}
```

- [ ] **Step 2: Write src/builder/templates/register_page.html.tmpl**

```
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Create Account</title>
  <link rel="stylesheet" href="/static/css/style.css">
</head>
<body class="bg-gray-50 text-gray-900 min-h-screen flex items-center justify-center">
  <div class="w-full max-w-sm space-y-6">
    <div class="text-center">
      <h1 class="text-2xl font-semibold tracking-tight">Create account</h1>
      <p class="text-sm text-gray-500 mt-1">Already have an account?
        <a href="/static/pages/login.html" class="underline">Sign in</a>
      </p>
    </div>
    <div class="bg-white border border-gray-200 rounded-lg p-6 space-y-4">
      <div id="error-msg" class="hidden border border-red-200 bg-red-50 text-red-700 text-sm px-4 py-3 rounded"></div>
      <form id="register-form" class="space-y-4">
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Name</label>
          <input type="text" id="name" name="name" required
            class="block w-full border border-gray-300 rounded px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-gray-900">
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Email</label>
          <input type="email" id="email" name="email" required
            class="block w-full border border-gray-300 rounded px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-gray-900">
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700 mb-1">Password</label>
          <input type="password" id="password" name="password" required minlength="8"
            class="block w-full border border-gray-300 rounded px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-gray-900">
        </div>
        <button type="submit"
          class="w-full px-4 py-2 bg-gray-900 text-white text-sm rounded hover:bg-gray-700 transition-colors">
          Create account
        </button>
      </form>
    </div>
  </div>
  <script type="module" src="/static/js/register.js"></script>
</body>
</html>
```

- [ ] **Step 3: Write src/builder/templates/register.js.tmpl**

```
import { post } from '/static/js/lib/api.js';
import { redirectIfAuthed } from '/static/js/lib/auth.js';

const form = document.getElementById('register-form');
const errorMsg = document.getElementById('error-msg');

async function init() {
  await redirectIfAuthed();

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    errorMsg.classList.add('hidden');
    const name = document.getElementById('name').value;
    const email = document.getElementById('email').value;
    const password = document.getElementById('password').value;

    const res = await post('/api/auth/register', { name, email, password });
    if (res.ok) {
      window.location.href = '/static/pages/home.html';
    } else {
      errorMsg.textContent = res.error ?? 'Registration failed.';
      errorMsg.classList.remove('hidden');
    }
  });
}

init();
```

- [ ] **Step 4: Add handleScaffoldRegistration to src/builder/main.go**

```go
func handleScaffoldRegistration(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	data := newData("user", nil)
	type fileSpec struct{ tmpl, out string }
	specs := []fileSpec{
		{"register_handler.go.tmpl", "/src/app/handlers/register.go"},
		{"register_page.html.tmpl", "/src/app/static/pages/register.html"},
		{"register.js.tmpl", "/src/app/static/js/register.js"},
	}
	results := []string{}
	for _, spec := range specs {
		if err := renderToFile(spec.tmpl, spec.out, data); err != nil {
			return errResult(err.Error()), nil
		}
		results = append(results, "Created: "+spec.out)
	}
	results = append(results, "\nAdd routes in main.go:\n"+
		"  r.Post(\"/api/auth/register\", handlers.RegisterPOST(database.Read, database.Write, appCache))")
	return mcp.NewToolResultText(strings.Join(results, "\n") + "\n\n" + runPatternChecks()), nil
}
```

- [ ] **Step 5: Commit**

```bash
git add src/builder/templates/register_handler.go.tmpl src/builder/templates/register_page.html.tmpl src/builder/templates/register.js.tmpl src/builder/main.go
git commit -m "feat: add scaffold_registration tool and register templates"
```

---

### Task 15: add_js_form Tool

The `add_js_form` tool injects a form setup function into an existing JS module at the `// @inject-forms` marker.

- [ ] **Step 1: Add handleAddJSForm to src/builder/main.go**

```go
func handleAddJSForm(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	page, _ := req.Params.Arguments["page"].(string)
	apiEndpoint, _ := req.Params.Arguments["api_endpoint"].(string)
	rawFields, _ := req.Params.Arguments["fields"].([]interface{})
	title, _ := req.Params.Arguments["title"].(string)
	submitLabel, _ := req.Params.Arguments["submit_label"].(string)
	if submitLabel == "" {
		submitLabel = "Submit"
	}
	if !isSafeIdent(page) {
		return errResult("invalid page name"), nil
	}

	endpointSlug := strings.TrimPrefix(apiEndpoint, "/api/")
	endpointSlug = strings.Trim(endpointSlug, "/")
	formName := toPascal(endpointSlug)
	if formName == "" {
		formName = toPascal(page) + "Form"
	}

	fields := parseFields(rawFieldsToStrings(rawFields))
	data := newData(page, fields)
	data.APIEndpoint = apiEndpoint
	data.SubmitLabel = submitLabel
	data.Title = title
	data.FormName = formName

	formCode, err := renderToString("js_form.js.tmpl", data)
	if err != nil {
		return errResult(err.Error()), nil
	}

	// Try pluralized then singular JS filename
	targetPath := "/src/app/static/js/" + toPlural(page) + ".js"
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		targetPath = "/src/app/static/js/" + page + ".js"
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		return errResult("target JS file not found: " + targetPath), nil
	}

	marker := "// @inject-forms"
	if !strings.Contains(string(content), marker) {
		return errResult("marker '// @inject-forms' not found in " + targetPath + ". Re-add the marker and try again."), nil
	}

	call := "setup" + formName + "Form(document.getElementById('forms-container'));\n" + marker
	updated := strings.Replace(string(content), marker, call, 1)
	updated += "\n\n" + formCode

	if err := os.WriteFile(targetPath, []byte(updated), 0644); err != nil {
		return errResult(err.Error()), nil
	}
	return mcp.NewToolResultText("Form injected into " + targetPath + "\n\n" + runPatternChecks()), nil
}
```

- [ ] **Step 2: Write src/builder/templates/js_form.js.tmpl**

```
function setup{{.FormName}}Form(container) {
  const wrapper = document.createElement('div');
  wrapper.className = 'border border-gray-200 rounded-lg p-4 bg-white space-y-3 mt-4';
  {{if .Title}}
  const titleEl = document.createElement('h3');
  titleEl.className = 'text-sm font-semibold text-gray-900';
  titleEl.textContent = '{{.Title}}';
  wrapper.appendChild(titleEl);
  {{end}}
  const form = document.createElement('form');
  form.className = 'space-y-3';
  {{range .Fields}}
  const {{.Name}}Label = document.createElement('label');
  {{.Name}}Label.className = 'block text-sm font-medium text-gray-700';
  {{.Name}}Label.textContent = '{{titleCase .Name}}';
  const {{.Name}}Input = document.createElement('input');
  {{.Name}}Input.type = '{{if eq .Type "password"}}password{{else}}text{{end}}';
  {{.Name}}Input.name = '{{.Name}}';
  {{.Name}}Input.className = 'mt-1 block w-full border border-gray-300 rounded px-3 py-2 text-sm focus:outline-none focus:ring-1 focus:ring-gray-900';
  {{.Name}}Input.required = true;
  form.appendChild({{.Name}}Label);
  form.appendChild({{.Name}}Input);
  {{end}}
  const submitBtn = document.createElement('button');
  submitBtn.type = 'submit';
  submitBtn.className = 'px-4 py-2 bg-gray-900 text-white text-sm rounded hover:bg-gray-700 transition-colors';
  submitBtn.textContent = '{{.SubmitLabel}}';
  form.appendChild(submitBtn);

  const errEl = document.createElement('p');
  errEl.className = 'text-sm text-red-600 hidden';
  form.appendChild(errEl);

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    submitBtn.disabled = true;
    errEl.classList.add('hidden');
    const data = { {{range .Fields}}{{.Name}}: {{.Name}}Input.value, {{end}} };
    const res = await post('{{.APIEndpoint}}', data);
    submitBtn.disabled = false;
    if (res.ok) {
      form.reset();
      if (typeof loadList === 'function') await loadList();
    } else {
      errEl.textContent = res.error ?? 'Something went wrong.';
      errEl.classList.remove('hidden');
    }
  });

  wrapper.appendChild(form);
  container.appendChild(wrapper);
}
```

- [ ] **Step 3: Commit**

```bash
git add src/builder/templates/js_form.js.tmpl src/builder/main.go
git commit -m "feat: add add_js_form tool and js_form.js.tmpl"
```

---

### Task 16: build_css and run_linter Tools

- [ ] **Step 1: Add handleBuildCSS and handleRunLinter to src/builder/main.go**

```go
func handleBuildCSS(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	minify, _ := req.Params.Arguments["minify"].(bool)
	args := []string{
		"-i", "/src/app/static/css/input.css",
		"-o", "/src/app/static/css/style.css",
		"--content", "/src/app/static/**/*.html,/src/app/static/**/*.js",
	}
	if minify {
		args = append(args, "--minify")
	}
	cmd := exec.CommandContext(ctx, "/usr/local/bin/tailwindcss", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errResult(fmt.Sprintf("tailwindcss failed:\n%s", string(out))), nil
	}
	return mcp.NewToolResultText("CSS compiled to /src/app/static/css/style.css"), nil
}

func handleRunLinter(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	cmd := exec.CommandContext(ctx, "go", "vet", "./...")
	cmd.Dir = "/src/app"
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errResult(fmt.Sprintf("go vet failed:\n%s", string(out))), nil
	}

	bannedPatterns := []struct{ pattern, message string }{
		{`db\.Exec\(fmt\.Sprintf`, "SQL injection risk: use prepared statements"},
		{`db\.Query\(fmt\.Sprintf`, "SQL injection risk: use prepared statements"},
		{`\.innerHTML\s*=`, "XSS risk: use textContent or createElement instead of innerHTML"},
	}

	violations := []string{}
	goFiles, _ := filepath.Glob("/src/app/handlers/*.go")
	jsFiles, _ := filepath.Glob("/src/app/static/js/*.js")
	for _, file := range append(goFiles, jsFiles...) {
		content, _ := os.ReadFile(file)
		for _, bp := range bannedPatterns {
			re := regexp.MustCompile(bp.pattern)
			if re.Match(content) {
				violations = append(violations, fmt.Sprintf("%s: %s", filepath.Base(file), bp.message))
			}
		}
	}

	if len(violations) > 0 {
		return errResult("Linter found issues:\n" + strings.Join(violations, "\n")), nil
	}
	return mcp.NewToolResultText("go vet + pattern checks passed"), nil
}
```

- [ ] **Step 2: Commit**

```bash
git add src/builder/main.go
git commit -m "feat: add build_css and run_linter tool handlers"
```

---

### Task 17: Builder Docker Verification

- [ ] **Step 1: Rebuild Docker image**

```bash
docker compose up -d --build
```

- [ ] **Step 2: Verify MCP server binary exists**

```bash
docker exec $(docker compose ps -q app) ls -la /usr/local/bin/mcp-server
# Expected: file listed with execute permission
```

- [ ] **Step 3: Verify inspect_app responds**

In Claude Code, run `/mcp` and confirm `gova-builder` is listed with all tools.

Then call `inspect_app` — expected output shows empty models/handlers/pages/routes.

- [ ] **Step 4: Smoke test scaffold_list end-to-end**

Run these MCP tool calls in sequence:
```
execute_sql: CREATE TABLE tasks (id INTEGER PRIMARY KEY, title TEXT NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)
scaffold_list: name=task, fields=["title:string"]
build_css
```

Then verify:
```bash
# Files generated
docker exec $(docker compose ps -q app) ls /src/app/models/Task.go
docker exec $(docker compose ps -q app) ls /src/app/handlers/task_list.go
docker exec $(docker compose ps -q app) ls /src/app/static/pages/tasks.html
docker exec $(docker compose ps -q app) ls /src/app/static/js/tasks.js

# Go compiles
docker exec $(docker compose ps -q app) sh -c "cd /src/app && go build ./..."
```

Expected: all files present, `go build` succeeds.

- [ ] **Step 5: Commit**

```bash
git add .
git commit -m "feat: complete MCP builder (all tools and templates verified)"
```

---

## Phase 3: Harness Layer

### Task 18: CLAUDE.md, GEMINI.md, AGENTS.md

**Files:**
- Create: `CLAUDE.md`
- Create: `GEMINI.md`
- Create: `AGENTS.md`

- [ ] **Step 1: Write CLAUDE.md**

```markdown
> **Automated Workflow:** This project uses `/build` to build from `SEED.md` and `/launch` to deploy. Run `/build` to start.

# Claude Code Context: GOVA Monolith

You are the **Lead Architect** of a GOVA Monolith. Your goal is to build robust, secure web applications using the provided MCP "Factory" tools.

## Mandatory Scaffolding Rule

**You MUST call the appropriate MCP tool BEFORE writing any Go handler or JS module.**
This is not optional. This is not a suggestion.

The sequence is always:
**MCP tool → generated file → customize generated file**

NEVER:
- Write a handler from scratch, then call MCP tools
- Skip `scaffold_list` because "it's simpler to just write it"
- Create a `.js` file manually without calling `create_page` or `scaffold_list` first
- Create any file in `handlers/` or `static/js/` without having just called an MCP tool

Subagents must confirm at the start of each task:
> "Which MCP tool scaffolds this?" → call it → then customize.

---

## The Golden Recipe

### 1. Database First
- Think: What data do I need?
- Action: Use `execute_sql` to create the table.
- Rule: ALWAYS use `id INTEGER PRIMARY KEY` (no AUTOINCREMENT).
- Example:
  ```sql
  CREATE TABLE projects (
      id INTEGER PRIMARY KEY,
      name TEXT NOT NULL,
      status TEXT DEFAULT 'active',
      created_at DATETIME DEFAULT CURRENT_TIMESTAMP
  );
  ```

### 2. Scaffold the Backbone
- **Option A (Standard List):** `scaffold_list(name='project', fields=['name:string', 'status:string'])`
- **Option B (Custom):** `create_model(name='project', ...)` + `create_page(filename='projects', ...)`
- **Option C (Auth — optional):** `scaffold_auth()` → `scaffold_registration()`

> **Auth is optional.** Skip Option C for public sites. `middleware.Auth` is passive — it reads a session cookie if present but never blocks on its own. Protect specific API endpoints with `middleware.RequireAuth`. Protect pages client-side by calling `requireAuth()` at the top of the JS module.

### 3. Add Forms
- Use `add_js_form(page='projects', api_endpoint='/api/projects_create', ...)` to inject creation forms.
- Edit `.js` files to add custom behavior.
- Edit `.html` files to adjust layout and structure.
- Keep Go handler logic in `handlers/`. HTML in `static/pages/`. JS in `static/js/`.

### 4. Compile CSS
- ALWAYS call `build_css()` after adding or changing HTML classes.

---

## Critical Constraints

1. **No Raw SQL in handlers.** Use model methods only.
   - Correct: `model.GetAll()`
   - Wrong: `db.Query("SELECT * FROM projects")`

2. **No HTML rendering in Go handlers.** All handlers return JSON.
   - Correct: `jsonOK(w, items)`
   - Wrong: `fmt.Fprintf(w, "<li>%s</li>", name)`

3. **JS Safety — Non-Negotiable:**
   - `NEVER`: `element.innerHTML = userValue` ← XSS vector
   - `ALWAYS`: `element.textContent = userValue` (for plain text)
   - `ALWAYS`: `createElement` + `setAttribute` (for structured HTML)
   - `NEVER`: `eval()` or `new Function()` with any external data
   - `ALWAYS`: Use `api.js` for all fetch — never write raw `fetch()` calls
   - `NEVER`: `console.log()` with tokens, passwords, or session data

4. **No Node.js / NPM.** Tailwind CLI standalone only. `api.js` and `auth.js` are in `static/js/lib/` — do not add CDN script tags.

5. **Security Built-in:**
   - **CSRF:** Double-submit cookie. `api.js` reads `csrf_token` cookie and sends `X-CSRF-Token` header automatically.
   - **Sessions:** Signed HMAC-SHA256 cookie. `middleware.SetSession(w, userID, 24*time.Hour)` on login. `middleware.ClearSession(w)` on logout.
   - **Auth (API):** `jsonError(w, "unauthorized", 401)` for unauthenticated requests — never redirect from an API handler.
   - **Auth (Pages):** Call `requireAuth()` at the top of protected JS modules.
   - **Rate Limiting:** Login uses `rate_limits` table (5 attempts / 15 min per IP).

---

## Infrastructure

| Layer | Detail |
|---|---|
| **Web server** | Go `net/http` via chi in `src/app/main.go`. No Nginx. |
| **Go app** | Rebuilt automatically by `air` on file changes. |
| **SQLite** | WAL mode at `/data/app.db` (Docker volume). |
| **Sessions** | Signed cookie (`gova_session`). No database hit per request. |
| **Cache** | In-process cache in `cache/cache.go`. Lost on restart — that's fine. |

---

## Tool Cheat Sheet

| Tool | When to use |
|---|---|
| `inspect_app` | **Before scaffolding** — existing models, handlers, JS pages, routes |
| `execute_sql` | Create tables — always before `create_model` |
| `create_model` | Data layer; table must exist first |
| `create_handler` | Single custom JSON endpoint stub |
| `create_page` | Full page: `.html` shell + `.js` module + Go handler stub |
| `scaffold_list` | Non-personalized list: model + JSON handler + `.html` + `.js` |
| `scaffold_auth` | User model, login/logout/me JSON endpoints, rate limiting |
| `scaffold_registration` | Registration endpoint — run after `scaffold_auth` |
| `add_js_form` | Inject creation form into existing `.js` module |
| `build_css` | After editing HTML classes — compiles Tailwind |
| `run_linter` | `go vet` + SQL injection + innerHTML XSS checks |

---

## Custom / Escape Hatch Pattern

When `scaffold_list` doesn't fit (filtered views, detail pages, dashboards):

```
1. execute_sql       → create the table
2. create_model      → generate the model
3. create_page       → html shell + js module + handler stub
4. create_handler    → POST/DELETE handler stubs as needed
5. edit handlers/    → implement TODO logic using model methods
6. edit static/js/   → fetch data, render DOM (never innerHTML for user data)
7. add_js_form       → inject form at // @inject-forms marker
8. build_css         → compile
9. run_linter        → verify
```

---

## Frontend Patterns

**JS module structure:**
```js
import { get, post, del } from '/static/js/lib/api.js';
import { requireAuth } from '/static/js/lib/auth.js'; // protected pages only

const listEl = document.getElementById('item-list');

export async function loadList() {
  const res = await get('/api/items');
  if (!res.ok) { listEl.textContent = 'Failed to load.'; return; }
  renderList(res.data ?? []);
}

function renderList(items) {
  listEl.innerHTML = '';           // safe: no user data here
  items.forEach(item => {
    const li = document.createElement('li');
    li.textContent = item.name;    // safe: textContent not innerHTML
    listEl.appendChild(li);
  });
}

// @inject-forms

async function init() {
  await loadList();
}
init();
```

**Error display:**
```js
const errEl = document.createElement('p');
errEl.className = 'text-sm text-red-600';
errEl.textContent = res.error ?? 'Something went wrong.'; // textContent — safe
```
```

- [ ] **Step 2: Write GEMINI.md and AGENTS.md with identical content**

Copy `CLAUDE.md` content verbatim to both `GEMINI.md` and `AGENTS.md`. These files serve different AI harnesses but contain the same project rules.

```bash
cp CLAUDE.md GEMINI.md && cp CLAUDE.md AGENTS.md
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md GEMINI.md AGENTS.md
git commit -m "feat: add CLAUDE.md, GEMINI.md, AGENTS.md project context files"
```

---

### Task 19: SEED.md, CHECKLIST.md, README.md

- [ ] **Step 1: Write SEED.md**

```markdown
# App Specification

> Fill this in before running `/build`. The AI will use this as the source of truth for brainstorming and implementation.

## App Name
[Your app name]

## What Does It Do?
[One paragraph describing the app's purpose and target user]

## Core Features
- [ ] Feature 1
- [ ] Feature 2
- [ ] Feature 3

## Auth
- [ ] User login required
- [ ] Public registration allowed

## External Integrations
- [ ] Payments (Stripe)
- [ ] AI / LLM (OpenRouter)
- [ ] Other: ___

## Design Notes
[Any specific UI or UX requirements. Otherwise the AI will follow the Uncodixify standard.]
```

- [ ] **Step 2: Write CHECKLIST.md**

```markdown
# Setup Checklist

## First-Time Setup
- [ ] Clone this repo
- [ ] Run the install script for your harness:
  - Claude Code: `./install-claude.sh`
  - OpenCode: `./install-opencode.sh`
  - Gemini CLI: `./install-gemini.sh`
- [ ] Open your AI tool in this directory
- [ ] Verify MCP tools are connected: `/mcp` → should show `gova-builder` tools

## Before `/build`
- [ ] `SEED.md` filled in with app name, features, auth requirements
- [ ] `.env` has all required API keys for integrations checked in SEED.md

## Before `/launch`
- [ ] App reviewed and working at `http://localhost:[APP_PORT]`
- [ ] `TUNNEL_TOKEN` set in `.env`
- [ ] Domain configured in Cloudflare dashboard (Zero Trust → Tunnels)
```

- [ ] **Step 3: Write README.md**

```markdown
# GOVA Monolith: AI-Second

A template repository for building AI-driven web applications with the GOVA stack.

**G**o · **V**anilla JS · **A**lpine-free · SQLite WAL

## Core Idea

The AI doesn't write the important code — it calls MCP tools that render deterministic templates. No HTMX, no Alpine.js, no Templ compile step. Go handles JSON API. Vanilla ES modules handle all DOM rendering.

**One container. One binary. One SQLite file.** No Redis, no MySQL, no Nginx, no frontend build step.

## Quick Start

```bash
cp env.example .env
# Edit .env: set APP_NAME, SESSION_SECRET (openssl rand -hex 32)
```

| Tool | Install | Context file | Commands |
|---|---|---|---|
| **Claude Code** | `./install-claude.sh` | `CLAUDE.md` | `/build`, `/launch` |
| **Gemini CLI** | `./install-gemini.sh` | `GEMINI.md` | `/build`, `/launch` |
| **OpenCode** | `./install-opencode.sh` | `AGENTS.md` | `/build`, `/launch` |

Then:
1. Fill in `SEED.md` with your app idea
2. Run `/build`
3. Review the running app at `http://localhost:[APP_PORT]`
4. Run `/launch` to go live via Cloudflare Tunnel

## Stack

| Layer | Technology |
|---|---|
| Language | Go 1.25 |
| Router | chi |
| Frontend | Vanilla ES modules (no bundler) |
| Database | SQLite (WAL mode) |
| CSS | Tailwind CLI |
| Sessions | Signed cookies (HMAC-SHA256) |
| Cache | In-process sync.Map |
| Deployment | Cloudflare Tunnel |

## Token Efficiency

Each feature costs ~1,000 tokens to scaffold — the MCP server renders templates, not the LLM.
```

- [ ] **Step 4: Commit**

```bash
git add SEED.md CHECKLIST.md README.md
git commit -m "feat: add SEED.md, CHECKLIST.md, README.md"
```

---

### Task 20: Claude Commands

**Files:**
- Create: `.claude/commands/build.md`
- Create: `.claude/commands/launch.md`
- Create: `.claude/commands/security/analyze.md`
- Create: `.claude/settings.local.json`

- [ ] **Step 1: Write .claude/commands/build.md**

```markdown
---
description: Build a new GOVA application from SEED.md — full automated workflow from spec to running app
---

You are running the GOVA build workflow. Read this file completely before taking any action.

---

## Step 1: Validate Context

Read `SEED.md`. If it is empty or contains only placeholder text, STOP and tell the developer to fill it in first.

Read `.env`. Verify `SESSION_SECRET` is set to something other than the placeholder. If it still says `change-me-to-32-random-bytes-before-use`, STOP and warn:

> "SESSION_SECRET in .env is still the placeholder value. Generate a secure secret before building:
> `openssl rand -hex 32`"

---

## Step 2: Brainstorm

Use the `superpowers:brainstorming` skill with the contents of `SEED.md` as input.

- Clarify the app's features and data model
- Confirm auth requirements, resource types, external integrations
- Wait for developer approval before proceeding

---

## Step 3: Write an Implementation Plan

Use the `superpowers:writing-plans` skill.

**Mandatory constraints for the plan:**
- Tasks are **MCP tool calls**, not Go code or JS written by hand
- Skip TDD — there is no test suite
- One task per feature: `execute_sql` → `scaffold_*` → `add_js_form` → `build_css`
- Follow the Golden Recipe from `CLAUDE.md` for every feature
- If a create form exists, plan edit + delete too (CRUD completeness)
- Plan steps scaffold first, then customize. Never plan "implement X handler" — always start with the MCP scaffold tool.

---

## Step 4: Create Feature Branch

Use `superpowers:using-git-worktrees` to create an isolated branch.

Derive branch name from app name in SEED.md: "Task Manager" → `build/task-manager`

---

## Step 5: Implement

Use `superpowers:subagent-driven-development` to execute the plan.

### Mandatory Scaffolding Rule for every subagent:

**YOU MUST call the appropriate MCP tool BEFORE writing any Go handler or JS module.**
This is not optional. This is not a suggestion.

The sequence is always: **MCP tool → generated file → customize generated file**

NEVER:
- Write a handler from scratch, then call MCP tools
- Skip `scaffold_list` because "it's simpler to just write it"
- Create a `.js` file manually without calling `create_page` or `scaffold_list` first
- Create any file in `handlers/` or `static/js/` without having just called an MCP tool

Subagents must confirm at the start of each task:
> "Which MCP tool scaffolds this?" → call it → then customize.

### Additional mandatory context for every subagent:
- Follow the Golden Recipe from CLAUDE.md
- Never write raw SQL in handler files — use model methods only
- Call `build_css()` after the final UI pass
- Use `uncodixify` skill before any UI work
- Use `context7` MCP for any external API documentation
- Do not add manual cache calls to model methods — caching is automatic
- JS safety: NEVER use `element.innerHTML = userValue` (XSS). ALWAYS use `element.textContent` for user-supplied text. ALWAYS use `createElement` for structured HTML.

---

## Step 5b: Stripe Webhook Registration (if SEED.md has Payments checked)

If `[x] Payments (Stripe)` is in SEED.md:

1. Read `APP_URL` from `.env`. If empty, STOP:
   > "APP_URL is not set. Set it to your production domain before registering the Stripe webhook."
2. Register webhook via Stripe MCP: endpoint `${APP_URL}/api/stripe_webhook`
3. Start local listener: `stripe listen --forward-to http://localhost:[APP_PORT]/api/stripe_webhook`
4. Extract local webhook secret → write to `.env` as `STRIPE_WEBHOOK_SECRET`
5. Fire test event: `stripe trigger payment_intent.succeeded`
6. Verify handler returns 200 in `docker compose logs app`
7. Stop listener, restore production secret to `.env`

---

## Step 6: Security Analysis

Run the `/security:analyze` command on `src/app/`.

---

## Step 7: Security Fixes (if needed)

If Critical, High, or Medium findings exist:
1. Write a targeted fix plan
2. Execute fixes
3. Re-run `/security:analyze`

---

## Step 8: Pre-Completion Verification

Use `superpowers:verification-before-completion`.

Verify:
- **Features:** All SEED.md features implemented? Auth-required pages call `requireAuth()`? No placeholder text?
- **CRUD:** If a create form exists, do edit and delete exist?
- **Architecture:** Tables via `execute_sql`? Models via `create_model`? No raw SQL in handlers? JS never uses `innerHTML` with user data? `build_css()` called? Linter passed?
- **Design:** `uncodixify` invoked? Titles set? Mobile-responsive?
- **App:** `docker compose logs app` shows no errors?
- **Environment:** New env vars documented in `env.example`? No hardcoded secrets?

---

## Step 9: Done

Report to the developer:

> **Build complete.**
>
> App running at: `http://localhost:[APP_PORT]`
> Branch: `build/[app-name]`
> Security report: `.security/SECURITY_REPORT.md`
>
> Next steps: review the running app, then run `/launch` to go live.
```

- [ ] **Step 2: Write .claude/commands/launch.md**

```markdown
---
description: Deploy the GOVA app live via Cloudflare Tunnel
---

You are running the GOVA deployment workflow. Only run this after the developer has reviewed the running app from `/build`.

---

## Step 1: Check Prerequisites

Read `.env` and verify:

**1. `TUNNEL_TOKEN`** — must exist and be non-empty.
If missing, STOP: "TUNNEL_TOKEN is missing from .env. Create a tunnel at dash.cloudflare.com → Zero Trust → Tunnels."

**2. `APP_ENV`** — must be `production`.
If not, update it in `.env` automatically: `APP_ENV=production`
Then tell the developer: "APP_ENV set to production. Session cookies now require HTTPS. Do not revert for a live deployment."

**3. `APP_URL`** — should be set to the public domain.
If empty, warn (non-blocking): set APP_URL for Stripe webhooks / OAuth callbacks.

---

## Step 2: Add Cloudflare Tunnel to docker-compose.yml

If `docker-compose.yml` does not have a `tunnel:` service, append under `services:`:

```yaml
  tunnel:
    image: cloudflare/cloudflared:latest
    container_name: ${APP_NAME}-tunnel
    command: tunnel run
    restart: unless-stopped
    environment:
      - TUNNEL_TOKEN=${TUNNEL_TOKEN}
    networks:
      - default
```

---

## Step 3: Restart Containers

```bash
docker compose up -d
```

---

## Step 4: Verify Tunnel

```bash
docker compose logs tunnel
```
Expected: `connection registered` or `Registered tunnel connection`.

---

## Step 5: Report

> **Deployment complete.**
>
> Configure domain routing: Zero Trust → Tunnels → [your tunnel] → Public Hostname → `http://app:8080`
>
> Local access still available at: `http://localhost:[APP_PORT]`
```

- [ ] **Step 3: Write .claude/commands/security/analyze.md**

```markdown
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
```

- [ ] **Step 4: Write .claude/settings.local.json**

```json
{
  "permissions": {
    "allow": [
      "Bash(docker compose *)",
      "Bash(docker exec *)",
      "Bash(curl *)",
      "Bash(git *)",
      "Bash(openssl *)"
    ]
  }
}
```

- [ ] **Step 5: Create directories and commit**

```bash
mkdir -p .claude/commands/security
git add .claude/
git commit -m "feat: add Claude commands (build, launch, security/analyze)"
```

---

### Task 21: Gemini and OpenCode Commands

- [ ] **Step 1: Write .gemini/commands/build.toml**

```toml
description = "Build a new GOVA application from SEED.md — full automated workflow from spec to running app"
prompt = """
You are running the GOVA build workflow. Read this completely before taking any action.

## Step 1: Validate Context
Read SEED.md. If empty or placeholder, STOP and tell the developer to fill it in.
Read .env. If SESSION_SECRET is still the placeholder, STOP: "Generate: openssl rand -hex 32"

## Step 2: Brainstorm
Activate: activate_skill "superpowers:brainstorming"
Use SEED.md as input. Wait for developer approval.

## Step 3: Write Implementation Plan
Activate: activate_skill "superpowers:writing-plans"
Constraints: Tasks are MCP tool calls. No TDD. execute_sql → scaffold_* → add_js_form → build_css.
Follow the Golden Recipe from GEMINI.md. If create form exists, plan edit + delete too.

## Step 4: Feature Branch
Activate: activate_skill "superpowers:using-git-worktrees"
Branch from app name in SEED.md.

## Step 5: Implement
Activate: activate_skill "superpowers:subagent-driven-development"

MANDATORY SCAFFOLDING RULE FOR EVERY SUBAGENT:
YOU MUST call the appropriate MCP tool BEFORE writing any Go handler or JS module.
This is not optional. This is not a suggestion.
Sequence: MCP tool → generated file → customize generated file
NEVER write a handler or .js file without first calling the scaffold tool.
Subagents confirm: "Which MCP tool scaffolds this?" → call it → customize.

Additional mandatory context:
- Follow the Golden Recipe from GEMINI.md
- Never write raw SQL in handlers
- call build_css() after final UI pass
- Activate uncodixify: activate_skill "uncodixify"
- JS safety: NEVER innerHTML = userValue. ALWAYS textContent or createElement.

## Step 6: Security Analysis
Run /security:analyze on src/app/.

## Step 7: Security Fixes
If Critical/High/Medium findings: fix, re-run /security:analyze.

## Step 8: Verification
Activate: activate_skill "superpowers:verification-before-completion"
Check: all features implemented, no innerHTML XSS, build_css called, linter passed, no hardcoded secrets.

## Step 9: Done
Report: Build complete. App at http://localhost:[APP_PORT]. Run /launch to go live.
"""
```

- [ ] **Step 2: Write .gemini/commands/launch.toml**

```toml
description = "Deploy the GOVA app live via Cloudflare Tunnel"
prompt = """
You are running the GOVA deployment workflow. Only after developer has reviewed /build output.

## Step 1: Check .env
1. TUNNEL_TOKEN — must be non-empty. If missing, STOP with instructions.
2. APP_ENV — set to "production" if not already. Warn: session cookies now require HTTPS.
3. APP_URL — warn if empty (non-blocking).

## Step 2: Add Cloudflare Tunnel to docker-compose.yml
If no tunnel service exists, append under services:
  tunnel:
    image: cloudflare/cloudflared:latest
    container_name: ${APP_NAME}-tunnel
    command: tunnel run
    restart: unless-stopped
    environment:
      - TUNNEL_TOKEN=${TUNNEL_TOKEN}
    networks:
      - default

## Step 3: docker compose up -d

## Step 4: Verify
docker compose logs tunnel — expected: "connection registered"

## Step 5: Report
Deployment complete. Configure: Zero Trust → Tunnels → Public Hostname → http://app:8080
"""
```

- [ ] **Step 3: Write .opencode/commands/build.md and launch.md**

Mirror `.claude/commands/build.md` and `.claude/commands/launch.md` content verbatim into `.opencode/commands/build.md` and `.opencode/commands/launch.md`.

```bash
mkdir -p .opencode/commands
cp .claude/commands/build.md .opencode/commands/build.md
cp .claude/commands/launch.md .opencode/commands/launch.md
```

- [ ] **Step 4: Commit**

```bash
mkdir -p .gemini/commands
git add .gemini/ .opencode/
git commit -m "feat: add Gemini and OpenCode commands (build, launch)"
```

---

### Task 22: Install Scripts

**Files:**
- Create: `install-claude.sh`
- Create: `install-gemini.sh`
- Create: `install-opencode.sh`
- Create: `opencode.json`

- [ ] **Step 1: Write install-claude.sh**

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLAUDE_DIR="$HOME/.claude"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BOLD='\033[1m'
NC='\033[0m'

ok()   { echo -e "  ${GREEN}✓${NC} $1"; }
warn() { echo -e "  ${YELLOW}!${NC} $1"; }
fail() { echo -e "  ${RED}✗${NC} $1"; exit 1; }
step() { echo -e "\n${BOLD}▶ $1${NC}"; }

echo ""
echo -e "${BOLD}GOVA Monolith — Claude Code Setup${NC}"
echo "======================================"

step "Checking prerequisites"
command -v docker >/dev/null 2>&1 || fail "docker not found — install Docker Desktop"
command -v git    >/dev/null 2>&1 || fail "git not found"
command -v curl   >/dev/null 2>&1 || fail "curl not found"
ok "docker, git, curl present"

command -v stripe >/dev/null 2>&1 \
    && ok "stripe CLI present" \
    || warn "stripe CLI not found — install for local webhook testing: https://stripe.com/docs/stripe-cli"

step "Setting up .env"

ENV_FILE="$SCRIPT_DIR/.env"
EXAMPLE_FILE="$SCRIPT_DIR/env.example"

set_env_var() {
    local file="$1" key="$2" value="$3"
    python3 - "$file" "$key" "$value" <<'PYEOF'
import sys
path, key, value = sys.argv[1], sys.argv[2], sys.argv[3]
with open(path) as f:
    lines = f.readlines()
lines = [f"{key}={value}\n" if l.startswith(f"{key}=") else l for l in lines]
with open(path, "w") as f:
    f.writelines(lines)
PYEOF
}

if [ ! -f "$ENV_FILE" ]; then
    cp "$EXAMPLE_FILE" "$ENV_FILE"
    ok "Copied env.example → .env"
else
    ok ".env already exists"
fi

CURRENT_APP_NAME=$(grep -E '^APP_NAME=' "$ENV_FILE" | head -1 | cut -d= -f2 | tr -d '"' | tr -d "'")
CURRENT_APP_NAME="${CURRENT_APP_NAME:-my-gova-app}"
printf "  App name [%s]: " "$CURRENT_APP_NAME"
read -r INPUT_APP_NAME </dev/tty
APP_NAME="${INPUT_APP_NAME:-$CURRENT_APP_NAME}"
set_env_var "$ENV_FILE" "APP_NAME" "$APP_NAME"
ok "APP_NAME set to: $APP_NAME"

CURRENT_SECRET=$(grep -E '^SESSION_SECRET=' "$ENV_FILE" | head -1 | cut -d= -f2 | tr -d '"' | tr -d "'")
if [ "$CURRENT_SECRET" = "change-me-to-32-random-bytes-before-use" ] || [ -z "$CURRENT_SECRET" ]; then
    SESSION_SECRET=$(openssl rand -hex 32)
    set_env_var "$ENV_FILE" "SESSION_SECRET" "$SESSION_SECRET"
    ok "SESSION_SECRET generated and written to .env"
else
    ok "SESSION_SECRET already set"
fi

CONTAINER_NAME="${APP_NAME}-app-1"
ok "Container: $CONTAINER_NAME"

step "Configuring ~/.claude/settings.json"

python3 - <<'PYEOF'
import json, os

settings_path = os.path.expanduser("~/.claude/settings.json")
try:
    with open(settings_path) as f:
        settings = json.load(f)
except (FileNotFoundError, json.JSONDecodeError):
    settings = {}

settings.setdefault("enabledPlugins", {})
if "superpowers@claude-plugins-official" not in settings["enabledPlugins"]:
    settings["enabledPlugins"]["superpowers@claude-plugins-official"] = True
    print("  + superpowers@claude-plugins-official added")
else:
    print("  - superpowers already registered")

if "mcpServers" in settings:
    del settings["mcpServers"]
    print("  ~ removed stale mcpServers from settings.json")

with open(settings_path, "w") as f:
    json.dump(settings, f, indent=2)
    f.write("\n")
PYEOF

ok "~/.claude/settings.json updated"

step "Registering Stripe MCP"

python3 - <<'PYEOF'
import json, os

claude_json_path = os.path.expanduser("~/.claude.json")
try:
    with open(claude_json_path) as f:
        config = json.load(f)
except (FileNotFoundError, json.JSONDecodeError):
    config = {}

config.setdefault("mcpServers", {})
if "stripe" not in config["mcpServers"]:
    config["mcpServers"]["stripe"] = {"type": "http", "url": "https://mcp.stripe.com/"}
    print("  + stripe MCP registered in ~/.claude.json")
else:
    print("  - stripe MCP already registered")

with open(claude_json_path, "w") as f:
    json.dump(config, f, indent=2)
    f.write("\n")
PYEOF

ok "Stripe MCP registered"

step "Building Docker image"

cd "$SCRIPT_DIR"
docker compose up -d --build
ok "Container up"

step "Verifying MCP server binary"

sleep 2
if docker exec "$CONTAINER_NAME" ls /usr/local/bin/mcp-server >/dev/null 2>&1; then
    ok "MCP server binary present at /usr/local/bin/mcp-server"
else
    fail "MCP server binary not found. Run: docker compose logs app"
fi

step "Generating .mcp.json"

python3 - "$CONTAINER_NAME" "$SCRIPT_DIR" <<'PYEOF'
import json, sys, os

container   = sys.argv[1]
project_dir = sys.argv[2]
mcp_path    = os.path.join(project_dir, ".mcp.json")

config = {
    "mcpServers": {
        "gova-builder": {
            "command": "docker",
            "args": ["exec", "-i", container, "/usr/local/bin/mcp-server"]
        }
    }
}

with open(mcp_path, "w") as f:
    json.dump(config, f, indent=2)
    f.write("\n")

print(f"  + .mcp.json → gova-builder via {container}")
PYEOF

ok ".mcp.json generated"

echo ""
echo "======================================"
echo -e "${GREEN}${BOLD}Setup complete!${NC}"
echo ""
echo "  1. Fill in SEED.md with your app idea"
echo "  2. Add API keys to .env if needed"
echo "  3. Open Claude Code:  claude"
echo "  4. Verify MCP tools:  /mcp"
echo "  5. Start building:    /build"
echo ""
```

- [ ] **Step 2: Write install-gemini.sh**

Copy `install-claude.sh` and replace Claude-specific steps with Gemini equivalents. The Gemini CLI reads `GEMINI.md`, uses `.gemini/settings.json`, and does not need `~/.claude.json` setup.

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BOLD='\033[1m'
NC='\033[0m'

ok()   { echo -e "  ${GREEN}✓${NC} $1"; }
warn() { echo -e "  ${YELLOW}!${NC} $1"; }
fail() { echo -e "  ${RED}✗${NC} $1"; exit 1; }
step() { echo -e "\n${BOLD}▶ $1${NC}"; }

echo ""
echo -e "${BOLD}GOVA Monolith — Gemini CLI Setup${NC}"
echo "======================================"

step "Checking prerequisites"
command -v docker >/dev/null 2>&1 || fail "docker not found"
command -v git    >/dev/null 2>&1 || fail "git not found"
command -v curl   >/dev/null 2>&1 || fail "curl not found"
command -v gemini >/dev/null 2>&1 || fail "gemini CLI not found — install from https://github.com/google-gemini/gemini-cli"
ok "docker, git, curl, gemini present"

step "Setting up .env"

ENV_FILE="$SCRIPT_DIR/.env"
EXAMPLE_FILE="$SCRIPT_DIR/env.example"

set_env_var() {
    local file="$1" key="$2" value="$3"
    python3 - "$file" "$key" "$value" <<'PYEOF'
import sys
path, key, value = sys.argv[1], sys.argv[2], sys.argv[3]
with open(path) as f:
    lines = f.readlines()
lines = [f"{key}={value}\n" if l.startswith(f"{key}=") else l for l in lines]
with open(path, "w") as f:
    f.writelines(lines)
PYEOF
}

if [ ! -f "$ENV_FILE" ]; then
    cp "$EXAMPLE_FILE" "$ENV_FILE"
    ok "Copied env.example → .env"
else
    ok ".env already exists"
fi

CURRENT_APP_NAME=$(grep -E '^APP_NAME=' "$ENV_FILE" | head -1 | cut -d= -f2 | tr -d '"' | tr -d "'")
CURRENT_APP_NAME="${CURRENT_APP_NAME:-my-gova-app}"
printf "  App name [%s]: " "$CURRENT_APP_NAME"
read -r INPUT_APP_NAME </dev/tty
APP_NAME="${INPUT_APP_NAME:-$CURRENT_APP_NAME}"
set_env_var "$ENV_FILE" "APP_NAME" "$APP_NAME"
ok "APP_NAME set to: $APP_NAME"

CURRENT_SECRET=$(grep -E '^SESSION_SECRET=' "$ENV_FILE" | head -1 | cut -d= -f2 | tr -d '"' | tr -d "'")
if [ "$CURRENT_SECRET" = "change-me-to-32-random-bytes-before-use" ] || [ -z "$CURRENT_SECRET" ]; then
    SESSION_SECRET=$(openssl rand -hex 32)
    set_env_var "$ENV_FILE" "SESSION_SECRET" "$SESSION_SECRET"
    ok "SESSION_SECRET generated"
else
    ok "SESSION_SECRET already set"
fi

CONTAINER_NAME="${APP_NAME}-app-1"

step "Configuring .gemini/settings.json"

mkdir -p "$SCRIPT_DIR/.gemini"
python3 - "$SCRIPT_DIR/.gemini/settings.json" <<'PYEOF'
import json, sys, os

path = sys.argv[1]
try:
    with open(path) as f:
        settings = json.load(f)
except (FileNotFoundError, json.JSONDecodeError):
    settings = {}

settings.setdefault("enabledPlugins", {})
if "superpowers@claude-plugins-official" not in settings["enabledPlugins"]:
    settings["enabledPlugins"]["superpowers@claude-plugins-official"] = True
    print("  + superpowers added to .gemini/settings.json")
else:
    print("  - superpowers already registered")

with open(path, "w") as f:
    json.dump(settings, f, indent=2)
    f.write("\n")
PYEOF

ok ".gemini/settings.json configured"

step "Building Docker image"
cd "$SCRIPT_DIR"
docker compose up -d --build
ok "Container up"

step "Verifying MCP server binary"
sleep 2
if docker exec "$CONTAINER_NAME" ls /usr/local/bin/mcp-server >/dev/null 2>&1; then
    ok "MCP server binary present"
else
    fail "MCP server binary not found. Run: docker compose logs app"
fi

step "Generating .mcp.json"
python3 - "$CONTAINER_NAME" "$SCRIPT_DIR" <<'PYEOF'
import json, sys, os

container   = sys.argv[1]
project_dir = sys.argv[2]
mcp_path    = os.path.join(project_dir, ".mcp.json")

config = {
    "mcpServers": {
        "gova-builder": {
            "command": "docker",
            "args": ["exec", "-i", container, "/usr/local/bin/mcp-server"]
        }
    }
}

with open(mcp_path, "w") as f:
    json.dump(config, f, indent=2)
    f.write("\n")
print(f"  + .mcp.json → gova-builder via {container}")
PYEOF

ok ".mcp.json generated"

echo ""
echo "======================================"
echo -e "${GREEN}${BOLD}Setup complete!${NC}"
echo ""
echo "  1. Fill in SEED.md with your app idea"
echo "  2. Add API keys to .env if needed"
echo "  3. Open Gemini CLI:   gemini"
echo "  4. Start building:    /build"
echo ""
```

- [ ] **Step 3: Write install-opencode.sh**

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BOLD='\033[1m'
NC='\033[0m'

ok()   { echo -e "  ${GREEN}✓${NC} $1"; }
warn() { echo -e "  ${YELLOW}!${NC} $1"; }
fail() { echo -e "  ${RED}✗${NC} $1"; exit 1; }
step() { echo -e "\n${BOLD}▶ $1${NC}"; }

echo ""
echo -e "${BOLD}GOVA Monolith — OpenCode Setup${NC}"
echo "======================================"

step "Checking prerequisites"
command -v docker   >/dev/null 2>&1 || fail "docker not found"
command -v git      >/dev/null 2>&1 || fail "git not found"
command -v curl     >/dev/null 2>&1 || fail "curl not found"
command -v opencode >/dev/null 2>&1 || fail "opencode not found — install from https://opencode.ai"
ok "docker, git, curl, opencode present"

step "Setting up .env"

ENV_FILE="$SCRIPT_DIR/.env"
EXAMPLE_FILE="$SCRIPT_DIR/env.example"

set_env_var() {
    local file="$1" key="$2" value="$3"
    python3 - "$file" "$key" "$value" <<'PYEOF'
import sys
path, key, value = sys.argv[1], sys.argv[2], sys.argv[3]
with open(path) as f:
    lines = f.readlines()
lines = [f"{key}={value}\n" if l.startswith(f"{key}=") else l for l in lines]
with open(path, "w") as f:
    f.writelines(lines)
PYEOF
}

if [ ! -f "$ENV_FILE" ]; then
    cp "$EXAMPLE_FILE" "$ENV_FILE"
    ok "Copied env.example → .env"
else
    ok ".env already exists"
fi

CURRENT_APP_NAME=$(grep -E '^APP_NAME=' "$ENV_FILE" | head -1 | cut -d= -f2 | tr -d '"' | tr -d "'")
CURRENT_APP_NAME="${CURRENT_APP_NAME:-my-gova-app}"
printf "  App name [%s]: " "$CURRENT_APP_NAME"
read -r INPUT_APP_NAME </dev/tty
APP_NAME="${INPUT_APP_NAME:-$CURRENT_APP_NAME}"
set_env_var "$ENV_FILE" "APP_NAME" "$APP_NAME"
ok "APP_NAME set to: $APP_NAME"

CURRENT_SECRET=$(grep -E '^SESSION_SECRET=' "$ENV_FILE" | head -1 | cut -d= -f2 | tr -d '"' | tr -d "'")
if [ "$CURRENT_SECRET" = "change-me-to-32-random-bytes-before-use" ] || [ -z "$CURRENT_SECRET" ]; then
    SESSION_SECRET=$(openssl rand -hex 32)
    set_env_var "$ENV_FILE" "SESSION_SECRET" "$SESSION_SECRET"
    ok "SESSION_SECRET generated"
else
    ok "SESSION_SECRET already set"
fi

CONTAINER_NAME="${APP_NAME}-app-1"

step "Writing opencode.json"
cat > "$SCRIPT_DIR/opencode.json" <<JSONEOF
{
  "model": "anthropic/claude-opus-4-7"
}
JSONEOF
ok "opencode.json written"

step "Building Docker image"
cd "$SCRIPT_DIR"
docker compose up -d --build
ok "Container up"

step "Verifying MCP server binary"
sleep 2
if docker exec "$CONTAINER_NAME" ls /usr/local/bin/mcp-server >/dev/null 2>&1; then
    ok "MCP server binary present"
else
    fail "MCP server binary not found. Run: docker compose logs app"
fi

step "Generating .mcp.json"
python3 - "$CONTAINER_NAME" "$SCRIPT_DIR" <<'PYEOF'
import json, sys, os

container   = sys.argv[1]
project_dir = sys.argv[2]
mcp_path    = os.path.join(project_dir, ".mcp.json")

config = {
    "mcpServers": {
        "gova-builder": {
            "command": "docker",
            "args": ["exec", "-i", container, "/usr/local/bin/mcp-server"]
        }
    }
}

with open(mcp_path, "w") as f:
    json.dump(config, f, indent=2)
    f.write("\n")
print(f"  + .mcp.json → gova-builder via {container}")
PYEOF

ok ".mcp.json generated"

echo ""
echo "======================================"
echo -e "${GREEN}${BOLD}Setup complete!${NC}"
echo ""
echo "  1. Fill in SEED.md with your app idea"
echo "  2. Add API keys to .env if needed"
echo "  3. Open OpenCode:     opencode"
echo "  4. Start building:    /build"
echo ""
```

- [ ] **Step 4: Make install scripts executable and commit**

```bash
chmod +x install-claude.sh install-gemini.sh install-opencode.sh
git add install-claude.sh install-gemini.sh install-opencode.sh opencode.json
git commit -m "feat: add install scripts for Claude, Gemini, OpenCode"
```

---

## Phase 4: Final Verification

### Task 23: End-to-End Verification

- [ ] **Step 1: Clean state — remove test artifacts from Task 17**

```bash
docker exec $(docker compose ps -q app) sh -c "rm -f /src/app/models/Task.go /src/app/handlers/task_list.go /src/app/static/pages/tasks.html /src/app/static/js/tasks.js"
docker exec $(docker compose ps -q app) sqlite3 /data/app.db "DROP TABLE IF EXISTS tasks;"
```

- [ ] **Step 2: Verify app builds clean**

```bash
docker compose up -d --build 2>&1 | tail -5
docker compose logs app 2>&1 | grep -E "listening|error|fatal"
# Expected: "GOVA app listening on :8080" — no errors
```

- [ ] **Step 3: Verify home page and static files serve**

```bash
curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/
# Expected: 200

curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/static/js/lib/api.js
# Expected: 200

curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/static/css/style.css
# Expected: 200
```

- [ ] **Step 4: Verify CSRF cookie is set**

```bash
curl -s -c /tmp/cookies.txt http://localhost:8080/ > /dev/null
grep csrf_token /tmp/cookies.txt
# Expected: line containing csrf_token
```

- [ ] **Step 5: Verify scaffold_auth end-to-end**

Call MCP tools in sequence (via Claude Code `/mcp` or direct MCP client):
```
scaffold_auth
scaffold_registration
```

Then wire routes in `src/app/main.go`:
```go
r.Post("/api/auth/login",    handlers.LoginPOST(database.Read, database.Write, appCache))
r.Post("/api/auth/logout",   handlers.LogoutPOST())
r.Get("/api/auth/me",        handlers.MeGET(database.Read, database.Write, appCache))
r.Post("/api/auth/register", handlers.RegisterPOST(database.Read, database.Write, appCache))
```

Wait for air to rebuild, then:
```bash
# Register a user
curl -s -c /tmp/cookies.txt -b /tmp/cookies.txt \
  -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -H "X-CSRF-Token: $(grep csrf_token /tmp/cookies.txt | awk '{print $NF}')" \
  -d '{"name":"Test User","email":"test@example.com","password":"password123"}'
# Expected: {"ok":true,"data":{"id":1}}

# Check me endpoint
curl -s -b /tmp/cookies.txt http://localhost:8080/api/auth/me
# Expected: {"ok":true,"data":{"id":1,"name":"Test User","email":"test@example.com"}}

# Logout
curl -s -c /tmp/cookies.txt -b /tmp/cookies.txt \
  -X POST http://localhost:8080/api/auth/logout \
  -H "X-CSRF-Token: $(grep csrf_token /tmp/cookies.txt | awk '{print $NF}')"
# Expected: {"ok":true}

# Me should now return 401
curl -s -b /tmp/cookies.txt http://localhost:8080/api/auth/me
# Expected: {"ok":false,"error":"unauthorized"}
```

- [ ] **Step 6: Verify scaffold_list end-to-end**

```
execute_sql: CREATE TABLE notes (id INTEGER PRIMARY KEY, content TEXT NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)
scaffold_list: name=note, fields=["content:string"]
```

Wire route in `main.go`:
```go
r.Get("/api/notes", handlers.NoteListGET(database.Read, database.Write, appCache))
```

Then:
```bash
# Create a note directly in SQLite to test the list endpoint
docker exec $(docker compose ps -q app) sqlite3 /data/app.db \
  "INSERT INTO notes (content) VALUES ('Hello GOVA');"

curl -s http://localhost:8080/api/notes
# Expected: {"ok":true,"data":[{"id":1,"content":"Hello GOVA","created_at":"..."}]}

curl -s http://localhost:8080/static/pages/notes.html | head -5
# Expected: <!DOCTYPE html>
```

- [ ] **Step 7: Run linter**

```
run_linter
```
Expected: "go vet + pattern checks passed"

- [ ] **Step 8: Final commit**

```bash
# Revert test routes and files added during verification
git diff src/app/main.go
# Restore main.go to template state (remove test routes)
git checkout src/app/main.go

git status
git add -A
git commit -m "feat: complete GOVA monolith template — verified end-to-end"
```

- [ ] **Step 9: Verify install script**

```bash
# Test the install script in a temp location (optional but recommended)
bash -n install-claude.sh  # syntax check
bash -n install-gemini.sh
bash -n install-opencode.sh
# Expected: no errors
```

---

## Summary

Total tasks: 23 across 4 phases.

**Phase 1** (Tasks 1–6): Docker infrastructure, Go app core (db, cache, middleware, handlers, main.go), static frontend base (home.html, api.js, auth.js).

**Phase 2** (Tasks 7–17): MCP builder server — all tool handlers and Go/HTML/JS templates.

**Phase 3** (Tasks 18–22): Harness layer — CLAUDE.md/GEMINI.md/AGENTS.md, command files, install scripts.

**Phase 4** (Task 23): End-to-end verification with real HTTP requests.
