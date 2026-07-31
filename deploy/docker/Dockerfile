# syntax=docker/dockerfile:1
#
# Image cnpg-console — multi-stage : build SPA (Vue/Vite) -> build Go (embed) -> distroless.
# Build depuis la racine du repo :
#   docker build -f deploy/docker/Dockerfile -t cnpg-console:dev .

# ── Stage 1 : build de la SPA Vue vers internal/web/dist ─────────────────────
FROM node:22-alpine AS web
WORKDIR /src
COPY web/package.json web/package-lock.json* ./web/
RUN cd web && npm install --no-audit --no-fund
COPY web/ ./web/
RUN mkdir -p internal/web/dist && cd web && npm run build

# ── Stage 2 : build du binaire Go (embarque internal/web/dist) ───────────────
FROM golang:1.24-alpine AS build
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-trimpath
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/web/dist/ ./internal/web/dist/
ARG VERSION=docker
RUN go build -ldflags="-s -w -X main.version=${VERSION}" -o /out/cnpg-console ./cmd/cnpg-console

# ── Stage 3 : runtime minimal (non-root, sans shell) ─────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/cnpg-console /app/cnpg-console
ENV CNPG_CONFIG=/etc/cnpg-console/config.yaml \
    CNPG_COOKIE_SECURE=true
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/cnpg-console"]
