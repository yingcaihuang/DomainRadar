# =============================================================================
# Stage 1: Build
# =============================================================================
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source and compile
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -mod=mod \
    -ldflags="-s -w" \
    -o /app/server \
    ./cmd/server/

# =============================================================================
# Stage 2: Production
# =============================================================================
FROM alpine:3.21 AS production

RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -S appgroup \
    && adduser -S appuser -G appgroup

WORKDIR /app

COPY --from=builder /app/server .

RUN chown -R appuser:appgroup /app

USER appuser

EXPOSE 8080

ENTRYPOINT ["./server"]

# =============================================================================
# Stage 3: Development (hot-reload with air + delve debugger)
# =============================================================================
FROM golang:1.26-alpine AS development

RUN apk add --no-cache git ca-certificates tzdata

# Install air for hot-reload and delve for debugging
RUN go install github.com/air-verse/air@latest \
    && go install github.com/go-delve/delve/cmd/dlv@latest

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Source is volume-mounted in development, but copy for initial build
COPY . .

# Expose application port and delve debug port
EXPOSE 8080 2345

CMD ["air", "-c", ".air.toml"]
