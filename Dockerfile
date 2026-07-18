# ============================================================
# Stage 1: Build Go webserver
# ============================================================
FROM golang:1.22-alpine AS go-builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o webserver ./cmd/webserver/

# ============================================================
# Stage 2: Build frontend (SPA)
# ============================================================
FROM node:22-alpine AS frontend-builder
WORKDIR /build
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci
COPY frontend/ .
RUN npm run build

# ============================================================
# Stage 3: Runtime image — two flavors
# If OPENROUTER_ENABLED=true, Python is NOT needed.
# We bundle it anyway for flexibility; the entrypoint is Go.
# ============================================================
FROM python:3.11-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Go webserver binary
COPY --from=go-builder /build/webserver .

# Frontend SPA
COPY --from=frontend-builder /build/dist ./frontend/dist

# Python model backend + tokenizer backend
COPY python/ ./python/
RUN pip install --no-cache-dir transformers torch tokenizers 2>/dev/null || true

# Pre-cache the default GPT-2 tokenizer so the server starts without HF download
RUN python3 -c "from transformers import AutoTokenizer; AutoTokenizer.from_pretrained('openai-community/gpt2')" 2>/dev/null || true

ENV PORT=8080
ENV STATIC_DIR=/app/frontend/dist
ENV LOCAL_MODEL=openai-community/gpt2

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD curl -sf http://localhost:8080/health || exit 1

CMD ["./webserver"]
