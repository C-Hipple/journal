# Stage 1: Build Frontend
FROM node:18-alpine AS frontend-builder
WORKDIR /app/frontend
# Copy package files first to leverage cache
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install
# Copy the rest of the frontend source code
COPY frontend/ ./
RUN npm run build

# Stage 2: Build Backend
FROM golang:alpine AS backend-builder
WORKDIR /app
# Install git just in case it's needed for dependencies
RUN apk add --no-cache git
COPY go.mod ./
# Download dependencies (if any)
RUN go mod download
COPY main.go ./
# Build the Go binary
RUN go build -o journal main.go

# Stage 3: Final Production Image
FROM alpine:latest
WORKDIR /app

# Install runtime dependencies:
# - git: for git sync functionality
# - openssh-client: for git operations over SSH
# - ca-certificates: for making HTTPS requests to Gemini API
RUN apk add --no-cache git openssh-client ca-certificates

# Copy the binary from the backend builder
COPY --from=backend-builder /app/journal .

# Copy the frontend build artifacts from the frontend builder
# The application expects them in ./frontend/build relative to the working directory
COPY --from=frontend-builder /app/frontend/build ./frontend/build

# Expose the application port
EXPOSE 8080

# Environment variables should be passed at runtime:
# - JOURNAL_PASSWORD
# - GEMINI_API_TOKEN
# - GIT_USERNAME
# - GIT_REPO_NAME

# Run the application
ENTRYPOINT ["./journal"]
