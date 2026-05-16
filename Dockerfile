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
