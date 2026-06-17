# Stage 1: Build the statically linked Go binary
FROM golang:1.25.5-alpine AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy Go module files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire source code (including embedded data)
COPY . .

# Compile the binary with CGO disabled and strip symbols to minimize container size
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o eldamo-mcp-server .

# Stage 2: Create a minimal, highly secure runtime container
FROM gcr.io/distroless/static-debian12

WORKDIR /

# Copy the compiled static binary from the builder stage
COPY --from=builder /app/eldamo-mcp-server /eldamo-mcp-server

# Cloud Run injects the PORT environment variable and expects the container to listen on it
EXPOSE 8080

# Run the MCP server
ENTRYPOINT ["/eldamo-mcp-server"]
