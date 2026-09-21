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

WORKDIR /app

# Upstream v2.1.x: npm workspaces with a root lockfile -> reproducible installs
COPY package.json package-lock.json ./
COPY frontend/package.json ./frontend/
RUN npm ci --workspace=frontend --include=dev --ignore-scripts --no-audit

COPY frontend/ ./frontend/
WORKDIR /app/frontend
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

# Upstream v2.1.x ships DefaultVersion "0.0.0" and injects the real one at build
# time. VERSION comes from FORK_VERSION via build-lamanese.yml; an image without
# it must not be built (agents compare versions for self-update).
ARG VERSION=""
RUN VER="${VERSION#v}"; \
    if ! printf '%s' "$VER" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'; then \
      echo "ERROR: VERSION='$VER' missing or not MAJOR.MINOR.PATCH[-PRERELEASE]" >&2; exit 1; \
    fi; \
    go mod download && \
    CGO_ENABLED=0 go build -buildvcs=false \
      -ldflags="-s -w -X github.com/PatchMon/PatchMon/server-source-code/internal/config.DefaultVersion=$VER" \
      -o /app/patchmon-server ./cmd/server

# -----------------------------------------------------------------------------
# Stage 2b: SCAP Security Guide content (the server is the agents' only SSG
# source since upstream 86480699). Pin SSG_VERSION to avoid the GitHub API.
# -----------------------------------------------------------------------------
FROM alpine:3.23 AS ssg-content
ARG SSG_VERSION=""
RUN apk add --no-cache wget unzip jq \
    && VER="${SSG_VERSION}" \
    && if [ -z "${VER}" ]; then \
         VER=$(wget -qO- https://api.github.com/repos/ComplianceAsCode/content/releases/latest | jq -r '.tag_name' | sed 's/^v//'); \
       fi \
    && if [ -z "${VER}" ] || [ "${VER}" = "null" ]; then \
         echo "ERROR: could not resolve SSG version; pass --build-arg SSG_VERSION=x.y.z" >&2; exit 1; \
       fi \
    && wget -q "https://github.com/ComplianceAsCode/content/releases/download/v${VER}/scap-security-guide-${VER}.zip" -O /tmp/ssg.zip \
    && mkdir -p /tmp/ssg-extract /ssg-content \
    && unzip -q /tmp/ssg.zip -d /tmp/ssg-extract \
    && find /tmp/ssg-extract -name 'ssg-*-ds.xml' -exec cp {} /ssg-content/ \; \
    && echo "${VER}" > /ssg-content/.ssg-version \
    && rm -rf /tmp/ssg.zip /tmp/ssg-extract

# -----------------------------------------------------------------------------
# Stage 3: minimal runtime
# -----------------------------------------------------------------------------
# Alpine 3.21 is end of life on 2026-11-01; 3.23 is supported until 2027-11-01.
FROM alpine:3.23

RUN apk add --no-cache wget ca-certificates tzdata

WORKDIR /app

# Server binary (DB migrations and frontend are embedded in the binary)
COPY --from=server-builder /app/patchmon-server ./

COPY --from=ssg-content /ssg-content ./ssg-content/

# Prebuilt agent binaries -> /app/agents (install/enroll scripts are go:embed since v2.1.x)
COPY --chmod=755 agents-prebuilt/patchmon-agent-* ./agents/

# Entrypoint (starts ./patchmon-server)
COPY --chmod=755 docker/backend.docker-entrypoint.sh ./entrypoint.sh

ENV PORT=3000
ENV AGENTS_DIR=/app/agents
ENV SSG_CONTENT_DIR=/app/ssg-content
# Cap Go heap to reduce RAM (override at runtime if needed, e.g. GOMEMLIMIT=128MiB)
ENV GOMEMLIMIT=256MiB

EXPOSE 3000

HEALTHCHECK --interval=10s --timeout=5s --start-period=30s --retries=5 \
  CMD wget -q -O /dev/null http://localhost:${PORT:-3000}/health || exit 1

ENTRYPOINT ["./entrypoint.sh"]
