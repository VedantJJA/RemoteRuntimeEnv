#!/bin/sh
set -e

# If Docker daemon is not running and dockerd is available (e.g. DinD privileged mode on PaaS)
if [ ! -S /var/run/docker.sock ] && command -v dockerd >/dev/null 2>&1; then
    echo "Starting internal Docker daemon (DinD)..."
    dockerd > /var/log/dockerd.log 2>&1 &
    
    # Wait for docker socket to be ready (up to 30s)
    TIMEOUT=30
    while [ $TIMEOUT -gt 0 ]; do
        if docker info >/dev/null 2>&1; then
            echo "Docker daemon initialized successfully."
            break
        fi
        sleep 1
        TIMEOUT=$((TIMEOUT - 1))
    done

    if [ $TIMEOUT -eq 0 ]; then
        echo "Warning: Timed out waiting for Docker daemon. Check /var/log/dockerd.log"
    fi
fi

# Ensure sandbox images (python, cpp, go, node) are built in Docker
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
    if [ -d "/app/images" ]; then
        echo "Checking and preparing sandbox container images..."
        for lang in python cpp go node; do
            if [ -d "/app/images/$lang" ]; then
                if ! docker image inspect "rre-$lang:latest" >/dev/null 2>&1; then
                    echo "Building sandbox image rre-$lang:latest..."
                    docker build -t "rre-$lang:latest" "/app/images/$lang"
                else
                    echo "Sandbox image rre-$lang:latest already present."
                fi
            fi
        done
    fi
else
    echo "Warning: Docker daemon is not reachable at /var/run/docker.sock."
    echo "Ensure Docker socket is mounted or container has privileged access for DinD."
fi

# Execute the main RRE server binary
exec /app/server "$@"
