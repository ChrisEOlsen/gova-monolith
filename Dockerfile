FROM golang:1.25 AS builder

RUN apt-get update && apt-get install -y --no-install-recommends gcc curl git && rm -rf /var/lib/apt/lists/*

# Tailwind CSS standalone binary
RUN ARCH=$(uname -m) && \
    if [ "$ARCH" = "aarch64" ]; then TW_ARCH="linux-arm64"; else TW_ARCH="linux-x64"; fi && \
    curl -sL "https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-${TW_ARCH}" \
        -o /usr/local/bin/tailwindcss \
    && chmod +x /usr/local/bin/tailwindcss

# Build the gova CLI
WORKDIR /src/builder
COPY src/builder/ ./
RUN go mod tidy
RUN CGO_ENABLED=1 go build -o /usr/local/bin/gova .

# Pre-download app dependencies
WORKDIR /src/app
COPY src/app/ ./
RUN go mod tidy

# ---- app: unchanged runtime, live-builds the Go app on every container start ----
FROM builder AS app
WORKDIR /src/app
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
EXPOSE 8080
CMD ["/entrypoint.sh"]

# ---- builder: the gova CLI, in a container that stays up so we can exec into it.
# Kept separate from `app` so `docker compose restart app` cannot kill a
# scaffold mid-write.
FROM debian:12-slim AS builder-cli
COPY --from=builder /usr/local/bin/gova /usr/local/bin/gova
CMD ["sleep", "infinity"]
