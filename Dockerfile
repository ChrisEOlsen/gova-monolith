FROM golang:1.25 AS builder

RUN apt-get update && apt-get install -y --no-install-recommends gcc curl git && rm -rf /var/lib/apt/lists/*

# Tailwind CSS standalone binary
RUN ARCH=$(uname -m) && \
    if [ "$ARCH" = "aarch64" ]; then TW_ARCH="linux-arm64"; else TW_ARCH="linux-x64"; fi && \
    curl -sL "https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-${TW_ARCH}" \
        -o /usr/local/bin/tailwindcss \
    && chmod +x /usr/local/bin/tailwindcss

# Build MCP server binary
WORKDIR /src/builder
COPY src/builder/ ./
RUN go mod tidy
RUN CGO_ENABLED=1 go build -o /usr/local/bin/mcp-server .

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

# ---- mcp: nothing but the compiled binary + glibc for the cgo sqlite3 driver ----
FROM gcr.io/distroless/base-debian12 AS mcp
COPY --from=builder /usr/local/bin/mcp-server /usr/local/bin/mcp-server
ENTRYPOINT ["/usr/local/bin/mcp-server"]
