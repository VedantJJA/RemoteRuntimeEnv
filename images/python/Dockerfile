# Minimal, no network tools, no package manager cache left behind.
# Runs as a non-root user so even a container-escape bug has no privileges.
FROM python:3.12-alpine
RUN adduser -D -u 1000 sandbox
USER sandbox
WORKDIR /sandbox
