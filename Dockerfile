# ==========================================
# STAGE 1: The Builder
# ==========================================
FROM golang:1.26 AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy dependency files first (for optimal Docker caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build a statically linked binary
# CGO_ENABLED=0 removes dependencies on the host OS C libraries
# -ldflags="-w -s" strips debug symbols to make the file even smaller
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o vaultpay ./cmd/api

# ==========================================
# STAGE 2: The Production Image (Distroless)
# ==========================================
# Distroless contains NO shell, NO package manager, just the bare minimum to run code.
FROM gcr.io/distroless/static-debian12

WORKDIR /app

# Copy ONLY the compiled binary from Stage 1
COPY --from=builder /app/vaultpay .

# Expose our API port
EXPOSE 8080

# Command to run the executable
CMD ["./vaultpay"]