FROM golang:1.22-alpine AS go-builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o webserver ./cmd/webserver/

FROM node:22-alpine AS frontend-builder
WORKDIR /build
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci
COPY frontend/ .
RUN npm run build

FROM python:3.11-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Go webserver
COPY --from=go-builder /build/webserver .

# Frontend static files
COPY --from=frontend-builder /build/dist ./frontend/dist

# Python model support
COPY python/ ./python/
RUN pip install --no-cache-dir transformers torch --extra-index-url https://download.pytorch.org/whl/cpu

EXPOSE 8080

ENV PORT=8080
ENV LOCAL_MODEL=openai-community/gpt2

CMD ["./webserver"]
