# Stage 1: Build
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy dependency files
COPY go.mod ./
# RUN go mod download (stdlib only currently, but cached layer ready)

# Copy source code
COPY . .

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/server ./cmd/server

# Stage 2: Runtime
FROM alpine:3.21 AS runner

RUN apk --no-cache add ca-certificates tzdata

RUN adduser -D -u 1001 appuser
USER appuser

WORKDIR /app
COPY --from=builder /app/server /app/server

ENV PORT=8080
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:8080/api/v1/health || exit 1

CMD ["/app/server"]
