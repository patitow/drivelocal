# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy Go module files first (for better caching)
COPY server-go/go.mod server-go/go.sum ./
RUN go mod download

# Copy all Go source files
COPY server-go/*.go ./

# Build Go server
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o drivelocal-server .

# Client build stage
FROM node:22-alpine AS client-builder

WORKDIR /build

# Copy package files first (for better caching)
COPY client/package*.json ./
RUN npm ci --silent

# Copy all client files
COPY client/ ./

# Build client
# vite.config.js outputs to ../dist relative to client/, so from /build it creates /dist
RUN npm run build

# Verify dist was created (vite outputs to ../dist, so it's /dist)
RUN ls -la /dist || (echo "ERROR: dist directory not found after build!" && exit 1)

# Production stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    ffmpeg \
    wget \
    && rm -rf /var/cache/apk/*

# Create app directory
WORKDIR /app

# Copy Go binary from builder
COPY --from=builder /app/drivelocal-server .

# Copy client build from client-builder
# Vite outputs to ../dist relative to client/, so from /build it's /dist
COPY --from=client-builder /dist ./dist

# Verify dist was copied
RUN ls -la dist/ || (echo "ERROR: dist directory not copied!" && exit 1)

# Copy server data directory
COPY server-go/server ./server

# Create directories
RUN mkdir -p thumbnails cache server/data /data

# Create non-root user
RUN addgroup -g 1001 -S appuser && \
    adduser -S appuser -u 1001

# Change ownership
RUN chown -R appuser:appuser /app
USER appuser

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1/api/health || exit 1

# Expose port
EXPOSE 80

# Start application
CMD ["./drivelocal-server"]
