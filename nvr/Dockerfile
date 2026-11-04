# syntax=docker/dockerfile:1
# Multi-stage build: web app -> Go binary (web embedded) -> small runtime with ffmpeg.

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
# vite writes into ../server/web/dist (the Go embed folder)
RUN mkdir -p ../server/web && npm run build

FROM golang:1.26-alpine AS server
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web /src/server/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/nvrd ./cmd/nvrd

FROM alpine:3.22
# uid 1001 matches the owner of /DATA/nvr on umma (tarkiman).
RUN apk add --no-cache ffmpeg tzdata ca-certificates \
    && addgroup -g 1001 nvr && adduser -D -u 1001 -G nvr nvr
COPY --from=server /out/nvrd /usr/local/bin/nvrd
USER nvr
ENV TZ=Asia/Jakarta NVR_DATA_DIR=/data
EXPOSE 8090
# Probe whatever port NVR_LISTEN uses (default :8090).
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s \
    CMD port="${NVR_LISTEN:-:8090}"; wget -qO- "http://127.0.0.1:${port##*:}/api/v1/healthz" >/dev/null || exit 1
ENTRYPOINT ["nvrd"]
