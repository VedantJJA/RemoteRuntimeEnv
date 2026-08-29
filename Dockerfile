# -----------------------------------------------------------------------------
# Stage 1: Build Go server binary
# -----------------------------------------------------------------------------
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Install git and ca-certificates for fetching modules
RUN apk add --no-cache git ca-certificates

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source files and compile static binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/server ./cmd/server

# -----------------------------------------------------------------------------
# Stage 2: Runtime Environment
# -----------------------------------------------------------------------------
FROM docker:26-cli-alpine

# Install standard utilities, certificates, and DinD support for PaaS environments
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    docker \
    docker-dind \
    bash

WORKDIR /app

# Copy compiled binary from builder
COPY --from=builder /app/server /app/server

# Copy sandbox image definitions
COPY images /app/images

# Copy and setup entrypoint script
COPY deploy/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh /app/server

# Create volume directories for SQLite DB and temporary submission workspaces
RUN mkdir -p /var/lib/rre/submissions

# Default configuration environment variables
ENV RRE_WORKDIR=/var/lib/rre/submissions \
    RRE_DB=/var/lib/rre/rre.db \
    RRE_ADDR=:8080 \
    PORT=8080

EXPOSE 8080

ENTRYPOINT ["/app/entrypoint.sh"]
