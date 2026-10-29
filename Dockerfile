# syntax=docker/dockerfile:1.7
# ---------- 1. build the web UI ----------
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---------- 2. build the Go binary ----------
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
COPY third_party/pocket-core ./third_party/pocket-core
RUN cd third_party/pocket-core && sha256sum -c SHA256SUMS
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/pocket-learn ./cmd/pocket-learn

# ---------- 3. runtime ----------
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata curl \
 && addgroup -g 1000 app && adduser -u 1000 -G app -D -h /data app
COPY --from=build /out/pocket-learn /usr/local/bin/pocket-learn
ENV DATA_DIR=/data PORT=8080 HOST=0.0.0.0
VOLUME ["/data"]
EXPOSE 8080
USER app
WORKDIR /data
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD curl -fsS http://localhost:8080/healthz || exit 1
LABEL org.opencontainers.image.title="Pocket Learn" \
      org.opencontainers.image.description="Learn anything and keep it: flashcards with spaced repetition (FSRS), cloze and type-in cards, pictures and sounds, Anki and CSV import, cards made by AI from a text, a note or your highlights, streaks and statistics." \
      org.opencontainers.image.source="https://github.com/abjelosevic88/pocket-homelab-learn" \
      org.opencontainers.image.licenses="MIT"
ENTRYPOINT ["pocket-learn"]
