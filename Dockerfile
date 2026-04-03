# syntax=docker/dockerfile:1
# The Van Is Secure — single Go binary + embedded web UI.

FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/keep-it-mobile ./cmd/keep-it-mobile

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -g 65532 -S nonroot \
    && adduser -u 65532 -S -G nonroot -h /app nonroot

WORKDIR /app
COPY --from=build /out/keep-it-mobile /usr/local/bin/keep-it-mobile

RUN mkdir -p /tmp/van-img-cache /data && chown -R nonroot:nonroot /tmp/van-img-cache /data

USER nonroot:nonroot
EXPOSE 8080
ENV PORT=8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -q -O /dev/null "http://127.0.0.1:${PORT:-8080}/api/health" || exit 1

ENTRYPOINT ["/usr/local/bin/keep-it-mobile"]
