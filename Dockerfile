# Build Stage
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY main.go .
# Disable CGO for a fully static binary
RUN CGO_ENABLED=0 go build -o omni-tool main.go

# Runtime Stage
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/omni-tool .

# MCP servers run over stdio by default, so we just execute the binary
ENTRYPOINT ["/app/omni-tool"]