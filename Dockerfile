FROM golang:1.25 AS builder

# UID/GID of the user both images run as. The app and builder containers write
# into bind-mounted host directories (./src, ./data, ./logs), so the container
# user has to be one the host filesystem accepts. 1000 is the first ordinary
# user on a Linux host and is what Docker Desktop maps to on macOS. On a Linux
# host whose account is not 1000:
#
#   docker compose build --build-arg UID=$(id -u) --build-arg GID=$(id -g)
ARG UID=1000
ARG GID=1000

RUN apt-get update && apt-get install -y --no-install-recommends gcc curl git && rm -rf /var/lib/apt/lists/*

# Tailwind CSS standalone binary, pinned and verified.
#
# This used to fetch /releases/latest with no pin and no checksum, which put an
# unauthenticated third-party binary into the root of every build a downstream
# app ever runs. A pin does go stale — upgrading is deliberately two lines:
# bump TAILWIND_VERSION and replace both sums from that release's
# sha256sums.txt.
ARG TAILWIND_VERSION=v4.3.3
ARG TAILWIND_SHA256_ARM64=55fd0b241214eff3de1e8ee4f22796662f2d2e7a49bcfca7477cfd0bac398195
ARG TAILWIND_SHA256_X64=dc61b3ac6b8c9ca874c0cc4c57b2409791a64c5540404ca5f5367360babc313a
RUN set -eu; \
    ARCH=$(uname -m); \
    if [ "$ARCH" = "aarch64" ]; then TW_ARCH="linux-arm64"; TW_SHA="$TAILWIND_SHA256_ARM64"; \
    else TW_ARCH="linux-x64"; TW_SHA="$TAILWIND_SHA256_X64"; fi; \
    curl -fsSL "https://github.com/tailwindlabs/tailwindcss/releases/download/${TAILWIND_VERSION}/tailwindcss-${TW_ARCH}" \
        -o /usr/local/bin/tailwindcss; \
    echo "${TW_SHA}  /usr/local/bin/tailwindcss" | sha256sum -c -; \
    chmod +x /usr/local/bin/tailwindcss

RUN groupadd -g "$GID" gova && useradd -u "$UID" -g "$GID" -m -s /bin/sh gova

# Build the gova CLI
WORKDIR /src/builder
COPY src/builder/ ./
RUN go mod tidy
RUN CGO_ENABLED=1 go build -o /usr/local/bin/gova .

# Pre-download app dependencies
WORKDIR /src/app
COPY src/app/ ./
RUN go mod tidy

# The app container compiles Go at every start, so the module and build caches
# have to be writable by the unprivileged user or every restart recompiles the
# world into a directory it cannot write.
RUN mkdir -p /home/gova/.cache && chown -R "$UID:$GID" /home/gova /go

# ---- app: unchanged runtime, live-builds the Go app on every container start ----
FROM builder AS app
WORKDIR /src/app
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
USER gova
ENV HOME=/home/gova
EXPOSE 8080
CMD ["/entrypoint.sh"]

# ---- builder: the gova CLI, in a container that stays up so we can exec into it.
# Kept separate from `app` so `docker compose restart app` cannot kill a
# scaffold mid-write.
FROM debian:12-slim AS builder-cli
ARG UID=1000
ARG GID=1000
COPY --from=builder /usr/local/bin/gova /usr/local/bin/gova
RUN groupadd -g "$GID" gova && useradd -u "$UID" -g "$GID" -m -s /bin/sh gova
USER gova
CMD ["sleep", "infinity"]
