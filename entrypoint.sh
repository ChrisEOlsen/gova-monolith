#!/bin/sh
set -e

cd /src/app
/usr/local/bin/tailwindcss -i ./static/css/input.css -o ./static/css/style.css --minify
exec air -c .air.toml
