# syntax=docker/dockerfile:1

# Production image: one self-contained static Go binary that also serves the
# embedded Vue app under /app/ (CHRN-53).
# Build from the repo root:
#   docker build -f deploy/Dockerfile -t chronicle:latest .

# 1) Build the Vite SPA -> web/dist (embedded by the Go binary in step 2).
#    Bun is PINNED (the CHRN-53 decision comment, mirroring Lyceum's LYCM-71):
#    a floating "1-debian" can pull a newer bun whose module resolution
#    breaks vue-tsc on .vue imports. Keep in sync with web/package.json's
#    `packageManager` (the source of truth) and .github/workflows/ci.yml's
#    web job.
FROM oven/bun:1.3.14-debian AS web
WORKDIR /app/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

# 2) Build the static binary. Pure Go (pgx, net/http) => CGO disabled.
FROM golang:1.26-bookworm AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
#    Copy the freshly built dist over the checked-in placeholder before
#    `go build` so //go:embed picks up the real bundle rather than
#    web/dist/.gitkeep. Without this stage the binary still builds -- the
#    embed pattern always resolves -- and web.Handler() answers "web UI not
#    built" instead of serving the app.
COPY --from=web /app/web/dist ./web/dist

# Build identity. Both default to EMPTY rather than a placeholder: an image
# built outside the publish workflow must not be able to claim a release
# version. main.version maps empty back to "dev".
ARG VERSION=
ARG GIT_SHA=
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X main.version=${VERSION} \
      -X main.commit=${GIT_SHA}" \
    -o /out/chronicle ./cmd/chronicle

# 3) Runtime: slim Alpine with ca-certificates and wget (busybox) for the
#    compose healthcheck.
FROM alpine:3.23 AS runtime
RUN apk add --no-cache ca-certificates
COPY --from=build /out/chronicle /usr/local/bin/chronicle
ENV CHRONICLE_PORT=4009
EXPOSE 4009
ENTRYPOINT ["chronicle"]
CMD ["serve"]
