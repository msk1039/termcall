# Stage 1: Build the binary
# Use 1.25 to match the `go 1.25.6` directive in go.mod. Older toolchains
# would either fail outright or trigger a slow toolchain auto-download.
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the termcall-server binary. The server has no CGO dependency
# (pure Go + pion), so we build a static binary.
RUN CGO_ENABLED=0 GOOS=linux go build -o termcall-server ./cmd/termcall-server

# Stage 2: Create a minimal runtime image
# Pin the alpine version for reproducible builds instead of `:latest`.
FROM alpine:3.20

# Install tzdata for correct log timestamps and create a non-root user.
# All listen ports (8080, 3478, 49152-65535) are >1024, so a non-root user
# can bind to them. ca-certificates is intentionally omitted — the server
# makes no outbound HTTPS calls today (add it back if/when auto-IP detection
# or similar lands, see docs/self-hosting-analysis.md §9.2).
RUN apk --no-cache add tzdata && \
    adduser -D -u 1001 termcall

WORKDIR /app

# Copy the binary from the builder stage, owned by the non-root user.
COPY --from=builder --chown=termcall:termcall /app/termcall-server ./

USER termcall

# We don't expose ports in the Dockerfile because we use network_mode: host
# and the exact ports depend on the .env file anyway.

ENTRYPOINT ["./termcall-server"]
