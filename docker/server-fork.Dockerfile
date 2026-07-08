# =============================================================================
# Fork build (lamanese) — public base images only, no dhi.io / no private registries.
# Builds the frontend, embeds it into the Go server binary, and assembles a
# minimal Alpine runtime. Agent binaries are expected in agents-prebuilt/
# (populated by the build-lamanese.yml workflow before the image build).
# =============================================================================

# -----------------------------------------------------------------------------
# Stage 1: build the frontend (gets embedded into the Go binary)
# -----------------------------------------------------------------------------
FROM node:22-alpine AS frontend-builder

WORKDIR /app/frontend

COPY frontend/package*.json ./
RUN npm install --ignore-scripts --legacy-peer-deps --no-audit

COPY frontend/ ./
RUN npm run build

# -----------------------------------------------------------------------------
# Stage 2: build the Go server with the frontend embedded
# -----------------------------------------------------------------------------
FROM golang:1.26-alpine AS server-builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Server source
COPY server-source-code/ ./server/
# Built frontend into the embed directory
COPY --from=frontend-builder /app/frontend/dist ./server/cmd/server/static/frontend/dist

WORKDIR /app/server

RUN go mod download && \
    CGO_ENABLED=0 go build -buildvcs=false -ldflags="-s -w" -o /app/patchmon-server ./cmd/server

# -----------------------------------------------------------------------------
# Stage 3: minimal runtime
# -----------------------------------------------------------------------------
FROM alpine:3.21

RUN apk add --no-cache wget ca-certificates tzdata

WORKDIR /app

# Server binary (DB migrations and frontend are embedded in the binary)
COPY --from=server-builder /app/patchmon-server ./

# Agent scripts (install/remove/enroll) and prebuilt agent binaries -> /app/agents
COPY agents ./agents/
COPY --chmod=755 agents-prebuilt/patchmon-agent-* ./agents/

# Entrypoint (starts ./patchmon-server)
COPY --chmod=755 docker/backend.docker-entrypoint.sh ./entrypoint.sh

ENV PORT=3000
ENV AGENTS_DIR=/app/agents
# Cap Go heap to reduce RAM (override at runtime if needed, e.g. GOMEMLIMIT=128MiB)
ENV GOMEMLIMIT=256MiB

EXPOSE 3000

HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=5 \
  CMD wget -q -O /dev/null http://localhost:${PORT:-3000}/health || exit 1

ENTRYPOINT ["./entrypoint.sh"]
