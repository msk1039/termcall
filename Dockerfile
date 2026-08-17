FROM golang:1.26.6-alpine3.23@sha256:e57c41c1d5864341031181b0db34b9a537bb5773eb6428e4e5bdaea0f9135406 AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -buildvcs=false \
    -ldflags="-s -w" \
    -o /out/termcall-server \
    ./cmd/termcall-server

FROM alpine:3.23.5@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40

RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H -u 10001 termcall

WORKDIR /app
COPY --from=builder --chown=termcall:termcall /out/termcall-server /app/termcall-server

USER 10001:10001

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null "http://127.0.0.1:${TERMCALL_WS_PORT:-8080}/healthz" || exit 1

ENTRYPOINT ["/app/termcall-server"]
