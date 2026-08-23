# syntax=docker/dockerfile:1

# Production image: one self-contained static Go binary.
# Build from the repo root:
#   docker build -f deploy/Dockerfile -t chronicle:latest .

# 1) Build the static binary. Pure Go (pgx, net/http) => CGO disabled.
FROM golang:1.26-bookworm AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .

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

# 2) Runtime: slim Alpine with ca-certificates and wget (busybox) for the
#    compose healthcheck.
FROM alpine:3.20 AS runtime
RUN apk add --no-cache ca-certificates
COPY --from=build /out/chronicle /usr/local/bin/chronicle
ENV CHRONICLE_PORT=4009
EXPOSE 4009
ENTRYPOINT ["chronicle"]
CMD ["serve"]
