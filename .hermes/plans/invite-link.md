# Plan: Invite Link with Fixed TTL Session

## Описание
Альтернативный способ setup сессии: вместо ручного ввода общей фразы — создание
инвайт-ссылки с фиксированным временем жизни.

## Backend Changes

### Session Manager (`cmd/webserver/session_manager.go`)
- Новый метод `CreateInviteSession(token, topic string, duration time.Duration)`
- Fixed-TTL режим: таймер истечения без sliding-продления
- Тот же `SessionManager`, та же hard-wipe механика, другой TTL policy
- Сессия не требует ключа/фразы — идентификация по токену

### Handlers (`cmd/webserver/handlers.go`)
- `POST /api/v1/session/invite` — создание инвайт-сессии
  - Body: `{token, topic, duration_minutes}`
  - Token генерируется клиентом (Crypto.getRandomValues)
  - topic используется как conversation name для chain
- Поддержка `X-Session-Token` header (рядом с существующим cookie)
- `GET /api/v1/session/status` — показывает режим (persistent/invite) и оставшееся время

## Frontend Changes

### Setup tab — новый режим "Invite"
- Переключатель: "Secret Phrase" / "Invite Link"
- Invite mode: поле "Topic" + слайдер "Duration (minutes)" + кнопка "Generate Link"
- После генерации: показ ссылки + кнопка Copy + предупреждение
- Автоматический переход на Encode/Decode после создания

### URL fragment parsing
- На загрузке: `window.location.hash` → извлечение токена
- Отправка токена как `X-Session-Token` header
- Если токен есть — сразу переключение на encode/decode (режим invite)

### UI Warning
- При показе инвайт-ссылки: явное предупреждение
  "Не пересылайте эту ссылку через тот же канал, что и cover-text"

### Timer
- Показ оставшегося времени (фиксированное, не продлевается)
- При истечении: очистка интерфейса как при revoke

## Design Notes (design-taste)
- Invite link mode: отдельная карточка внутри Setup tab, с серым фоном-подложкой
- Слайдер длительности: shadcn Slider primitive
- Предупреждение: желтый/оранжевый alert-badge с иконкой
- Таймер: моноширинный красный отсчёт при < 60 секунд

## Architecture Decisions
- **Token generation**: клиентская (JS Crypto.getRandomValues, 32 байта → hex)
- **TTL policy**: fixed (не sliding), отдельный enum от persistent
- **Session lookup**: X-Session-Token header + cookie (dual support)
- **Fragment safety**: # не уходит на сервер, извлекается JS на клиенте
