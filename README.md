# GOVA Monolith

A template for building web apps with an AI assistant.

**Go · Vanilla JS · SQLite**

## What it is

Clone it and you already have a working web app: a Go server, a database, and a
complete sign-in system — registration, login, sessions, password hashing, rate
limiting, CSRF. None of that is something you ask for; it is committed code.

For everything else there is a small CLI called `gova`. You give it a table and
it writes the feature:

```bash
./gova sql -query "CREATE TABLE projects (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    status     TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);"

./gova resource -name project -fields name:string,status:string
```

That second command writes the database code, the API endpoints, a web page with
a form and delete buttons, and tests for all of it — then wires up the routes.
The AI customizes what it generated rather than writing it from scratch.

The point: **the AI decides what to build; templates decide how.** Generated
code arrives already wired, already tested, and already following the security
rules, so a feature costs about a thousand tokens instead of a thousand lines of
guesswork.

## Getting started

```bash
cp env.example .env          # set APP_NAME and SESSION_SECRET
./install-claude.sh          # or ./install-opencode.sh, or both
```

Then:

1. Describe your app in `SEED.md`
2. Run `/build` — the assistant designs it, plans it, and builds it
3. Open `http://localhost:8080`
4. Run `/launch` to put it online through a Cloudflare Tunnel

## The commands

Run `./gova help` for details.

| Command | What it makes |
|---|---|
| `gova inspect` | what exists right now, and anything out of sync |
| `gova sql` | a table |
| `gova model` | database code for a table |
| `gova handler` | one custom API endpoint |
| `gova page` | a web page (HTML + JS) |
| `gova resource` | the whole thing: data, API, page, form, tests |
| `gova regen` | re-apply `api.json` after you edit it by hand |

Everything generated requires a signed-in caller. Pass `-public` to open a route
to anonymous visitors, and `-owner` to scope a resource to the user who created
each row — the table needs a
`user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE` column, and
another user's row answers 404.

## How it fits together

**One file describes the app.** `src/app/api.json` lists every data model, API
endpoint and page. The CLI writes it, and the server's routing is generated from
it — so nobody hand-wires a route.

**Two containers.** `app` runs the server; `builder` holds the `gova` CLI. One
SQLite file underneath. No Redis, no Nginx, no frontend build step.

**Plain everything.** Go returns JSON. Vanilla ES modules render the page.
Tailwind does the styling. No framework, no bundler, no Node.

**The app is published on `127.0.0.1` by default.** The rate limiter trusts
private-range peers as reverse proxies, so anything that can reach the port can
claim any client address. Set `APP_BIND=0.0.0.0` in `.env` when you deliberately
want it on the LAN — testing from a physical phone, say. `/launch` does not need
it; the tunnel reaches the app over the compose network.

## What the installers set up

`install-claude.sh` and `install-opencode.sh` register two **remote, third-party
MCP servers** in your harness config — [Stripe](https://mcp.stripe.com/) (used by
`/build` when `SEED.md` asks for payments) and
[Context7](https://mcp.context7.com/mcp) (library documentation lookup) — and
enable web fetch and search for build subagents. They are useful defaults, not
requirements: a build works without them, and you can drop either entry from
`~/.claude.json` or `.opencode/opencode.json` if you would rather not extend
trust to them. The `gova` builder itself is a local CLI and is not an MCP server.

## iOS

[`gova-ios`](../gova-ios) is a companion template that turns an app built here
into a native iPhone app. It reads the same `api.json`, so you describe your data
once.

## Reference

- [`CLAUDE.md`](CLAUDE.md) — the rules the assistant works under
- [`docs/API-CONTRACT.md`](docs/API-CONTRACT.md) — what the API guarantees
- [`docs/DECISIONS.md`](docs/DECISIONS.md) — why the tricky parts are built the way they are

## Stack

| Layer | Choice |
|---|---|
| Language | Go 1.25 |
| Router | chi |
| Frontend | Vanilla ES modules |
| Database | SQLite (WAL) |
| CSS | Tailwind CLI |
| Auth | Signed cookies + bearer tokens |
| Deploy | Cloudflare Tunnel |
