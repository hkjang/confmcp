# syntax=docker/dockerfile:1

# confmcp ships as a single self-contained image: the React console (with its
# fonts) is embedded into the Go binary, so an air-gapped install needs only
# this image and a PostgreSQL database. Nothing is fetched at runtime.

# ---- Stage 1: build the web console -------------------------------------
FROM node:22-alpine AS web

WORKDIR /src/web
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 \
    npm_config_fund=false \
    npm_config_audit=false

COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY web/ ./
RUN npm run build

# ---- Stage 2: build the server ------------------------------------------
FROM golang:1.26-alpine AS server

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /src
ENV CGO_ENABLED=0 GOOS=linux GOFLAGS=-buildvcs=false

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
# The console built in stage 1 replaces the placeholder before embedding.
COPY --from=web /src/internal/webui/dist ./internal/webui/dist

ARG VERSION=0.0.0
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN go build -trimpath \
      -ldflags "-s -w \
        -X github.com/hkjang/confmcp/internal/version.Version=${VERSION} \
        -X github.com/hkjang/confmcp/internal/version.Commit=${COMMIT} \
        -X github.com/hkjang/confmcp/internal/version.BuildDate=${BUILD_DATE}" \
      -o /out/confmcp ./cmd/server

# ---- Stage 3: runtime ----------------------------------------------------
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata curl && \
    addgroup -g 10001 -S confmcp && \
    adduser -u 10001 -S -G confmcp -h /home/confmcp confmcp

ENV TZ=Asia/Seoul \
    CONFMCP_ADDR=:8080

COPY --from=server /out/confmcp /usr/local/bin/confmcp

USER confmcp
WORKDIR /home/confmcp
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/confmcp"]

ARG VERSION=0.0.0
LABEL org.opencontainers.image.title="confmcp" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.description="요청자 본인의 Confluence 권한으로만 문서를 읽고 쓰는 Confluence 7.2 MCP 게이트웨이 (Keycloak SSO, MCP OAuth, 승인 기반 변경)" \
      org.opencontainers.image.source="https://github.com/hkjang/confmcp" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.vendor="hkjang"
