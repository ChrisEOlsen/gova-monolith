---
description: Deploy the GOVA app live via Cloudflare Tunnel
---

You are running the GOVA deployment workflow. Only run this after the developer has reviewed the running app from `/build`.

---

## Step 1: Check Prerequisites

Read `.env` and verify:

**1. `SESSION_SECRET`** — must not be the placeholder.
If it is still `change-me-to-32-random-bytes-before-use`, STOP: "SESSION_SECRET in .env is still the placeholder value. This is public — anyone who's seen this template can forge session cookies for your live app. Generate a real secret before going live: `openssl rand -hex 32`"
(This is the same check `/build` runs, repeated here because `/launch` is what actually exposes the app to the public internet — don't rely on `/build` having been the only thing that ran.)

**2. `TUNNEL_TOKEN`** — must exist and be non-empty.
If missing, STOP: "TUNNEL_TOKEN is missing from .env. Create a tunnel at dash.cloudflare.com → Zero Trust → Tunnels."

**3. `APP_ENV`** — must be `production`.
If not, update it in `.env` automatically: `APP_ENV=production`
Then tell the developer: "APP_ENV set to production. Session cookies now require HTTPS. Do not revert for a live deployment."

**4. `APP_URL`** — should be set to the public domain.
If empty, warn (non-blocking): set APP_URL for Stripe webhooks / OAuth callbacks.

---

## Step 2: Add Cloudflare Tunnel to docker-compose.yml

If `docker-compose.yml` does not have a `tunnel:` service, append under `services:`:

```yaml
  tunnel:
    image: cloudflare/cloudflared:latest
    pull_policy: always
    command: tunnel run
    restart: unless-stopped
    environment:
      - TUNNEL_TOKEN=${TUNNEL_TOKEN}
```

`pull_policy: always` is load-bearing, not tidiness. `:latest` is only a tag —
`docker compose up -d` resolves it from the **local image cache** and never
checks the registry if any copy is already there. A machine that pulled
`cloudflared` once months ago runs that build forever while the compose file
reads `latest`. cloudflared is the wrong image to let rot: Cloudflare's edge
refuses connections from clients that fall too far behind, so the failure
arrives later as a tunnel that stops registering, with nothing in the compose
file pointing at the cause. `always` re-resolves the tag on every `up`.

Do **not** pin a version number here instead. This is a template — a pin
written today is the outdated version every project inherits tomorrow, which is
the exact failure this line prevents.

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

Confirm the pull actually landed a current build:

```bash
docker compose exec tunnel cloudflared --version
```

Compare against the newest release at
https://github.com/cloudflare/cloudflared/releases/latest. Versions are
`YYYY.M.N`, so staleness is readable at a glance. More than a few months behind
means the pull was skipped — check that `pull_policy: always` is present on the
`tunnel` service.

---

## Step 5: Report

> **Deployment complete.**
>
> Configure domain routing: Zero Trust → Tunnels → [your tunnel] → Public Hostname → `http://app:[APP_PORT]`
>
> Local access still available at: `http://localhost:[APP_PORT]`
