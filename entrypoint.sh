#!/bin/sh
set -e

cd /src/app
/usr/local/bin/tailwindcss -i ./static/css/input.css -o ./static/css/style.css --minify
CGO_ENABLED=1 go build -o /tmp/server .
exec /tmp/server
