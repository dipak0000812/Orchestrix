# Build stage
FROM golang:1.24-alpine AS builder


# Install build dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o orchestrix ./cmd/server

# Runtime stage
FROM alpine:3.22

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

RUN addgroup -S orchestrix && adduser -S orchestrix -G orchestrix

WORKDIR /app

# Copy the application and its required runtime configuration.
COPY --from=builder --chown=orchestrix:orchestrix /app/orchestrix ./orchestrix
COPY --from=builder --chown=orchestrix:orchestrix /app/configs ./configs

# Expose port
EXPOSE 8080

USER orchestrix

# Run
CMD ["./orchestrix"]
