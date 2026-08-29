#!/usr/bin/env bash
# Builds every sandbox image the server expects to find locally
# (see runner.Languages and PullImages in the Go code). Run this once on
# the deploy VM before starting the server, and again whenever a
# Dockerfile changes.
set -euo pipefail
cd "$(dirname "$0")"

docker build -t rre-python:latest ./python
docker build -t rre-cpp:latest    ./cpp
docker build -t rre-go:latest     ./go
docker build -t rre-node:latest   ./node

echo "All sandbox images built."
