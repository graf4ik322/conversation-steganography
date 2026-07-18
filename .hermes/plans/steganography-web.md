# Plan: Conversation Steganography Web Service

## Status: IN PROGRESS

## Цель
Форкнуть `nethical6/conversation-steganography` и обернуть в веб-сервис по ТЗ.

---

## Фаза 1 — Ядро (Go библиотека)

### ✅ 1.1 OpenRouterModel
- Файл: `openrouter_model.go`
- Реализует `LanguageModel` через OpenAI-совместимый API
- Использует локальный токенайзер (Python subprocess) для Tokenize/Detokenize
- Поток: `Next()` → POST /chat/completions c max_tokens=1, logprobs=true, top_logprobs=N
- Server-level конфиг через .env: `OPENROUTER_ENABLED=true/false`
- **Статус: ✅ ГОТОВО**

### ✅ 1.2 Python Tokenizer Backend
- Файл: `python/tokenizer_backend.py`
- Легковесный процесс (без модели, только токенайзер)
- Протокол line-delimited JSON
- **Статус: ✅ ГОТОВО**

### ✅ 1.3 Go Tokenizer Wrapper
- Файл: `python_tokenizer.go`
- Реализует `Tokenizer` интерфейс
- Управляет Python-подпроцессом
- **Статус: ✅ ГОТОВО**

---

## Фаза 2 — Web API Server (Go cmd)

### ⬜ 2.1 Session Manager
- Файл: `cmd/webserver/session_manager.go`
- Per-session изоляция (sync.Map + per-session timer)
- Sliding TTL + hard cap
- Zero-out ключа при revoke/expiry
- Консоль прозрачности (audit events → SSE)

### ⬜ 2.2 HTTP Handlers
- Файл: `cmd/webserver/handlers.go`
- POST /api/v1/session/start
- GET /api/v1/session/status
- POST /api/v1/message/encode
- POST /api/v1/message/decode
- POST /api/v1/session/revoke
- GET /api/v1/events (SSE — консоль прозрачности)

### ⬜ 2.3 Model Factory
- Файл: `cmd/webserver/model_factory.go`
- По env `OPENROUTER_ENABLED` выбирает:
  - `true` → OpenRouterModel + PythonTokenizer
  - `false` → ProcessModel (оригинальный Python+transformers)

### ⬜ 2.4 Main entry point
- Файл: `cmd/webserver/main.go`
- HTTP server, graceful shutdown
- Rate limiting на session.start
- TLS/HSTS, no-cache headers
- Нулевое логирование секретных данных

---

## Фаза 3 — Frontend

### ⬜ 3.1 Проект React + Vite + Tailwind + shadcn/ui
- `frontend/` в корне
- shadcn/ui примитивы: Card, Tabs, Button, Badge, ScrollArea, Textarea

### ⬜ 3.2 Три экрана (Tabs внутри Card)
1. **Setup** — conversation name + secret phrase + QR опция
2. **Encode** — textarea → "Generate cover text" → Copy
3. **Decode** — paste cover text → результат

### ⬜ 3.3 Консоль прозрачности
- Чёрная панель внизу (всегда видна)
- Моноширинный шрифт
- SSE-подписка на `GET /api/v1/events`
- Пульсирующий "live" индикатор
- Fade-in новых строк
- Очищается при revoke/expiry

### ⬜ 3.4 Design
- Светлая тема + чёрная консоль (исключение)
- design-taste-frontend подход
- Референс консоли: flclashx.app (монотонный лог)
- Анимации: Radix primitives (без кастомной библиотеки)

---

## Фаза 4 — DevOps

### ⬜ 4.1 Dockerfile
- Multi-stage: Go build → Python deps → frontend build
- Переменные окружения: OPENROUTER_API_KEY, OPENROUTER_MODEL, etc.

### ⬜ 4.2 GitHub Actions
- Build Docker image
- Push to registry

### ⬜ 4.3 Coolify deploy (future)

---

## Критерии готовности

- [ ] `go build ./...` проходит
- [ ] `go vet ./...` без ошибок
- [ ] `go test ./...` проходит (оригинальные тесты)
- [ ] frontend собирается (`npm run build`)
- [ ] API эндпоинты отвечают (curl-тест)
- [ ] Консоль прозрачности получает SSE события
- [ ] Zero-out ключа подтверждён
- [ ] Изоляция сессий подтверждена
