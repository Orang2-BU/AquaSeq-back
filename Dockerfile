FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

FROM alpine:3.23 AS runner
RUN apk add --no-cache ca-certificates tzdata poppler-utils \
    && addgroup -S -g 1001 app \
    && adduser -S -D -u 1001 -G app app \
    && mkdir -p /app/uploads \
    && chown app:app /app/uploads
WORKDIR /app
COPY --from=builder /server /app/server
# autoMigrate reads this path relative to WORKDIR; SQL is not embedded.
COPY --from=builder /app/sql/schema.sql /app/sql/schema.sql
ENV PORT=8080 FILES_DIR=/app/uploads
VOLUME ["/app/uploads"]
USER 1001:1001
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null "http://127.0.0.1:${PORT}/api/v1/health" || exit 1
CMD ["/app/server"]
