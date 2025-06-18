# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev

# Set work directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o mcp-cron ./cmd/mcp-cron

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN adduser -D -s /bin/sh mcpcron

# Set work directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/mcp-cron .

# Create data directory with proper permissions
RUN mkdir -p /data && chown mcpcron:mcpcron /data

# Switch to non-root user
USER mcpcron

# Expose port (for SSE mode)
EXPOSE 8080

# Set default database path to mounted volume
ENV MCP_CRON_DATABASE_PATH=/data/mcp-cron.db

# Run the application
CMD ["./mcp-cron"]
