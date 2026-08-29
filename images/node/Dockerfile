FROM node:20-alpine
RUN deluser --remove-home node 2>/dev/null || true && adduser -D -u 1000 sandbox
USER sandbox
WORKDIR /sandbox
