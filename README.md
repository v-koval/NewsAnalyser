# News Analyzer

Сервис сбора и анализа новостей, событий и аналитических статей с формированием
email-дайджестов на выбранном языке. Всю аналитику (поиск, перевод, пересказ)
выполняет Cursor Cloud Agents API.

## Стек

- Go 1.22 (стандартная `net/http` с 1.22-роутингом, `pgx/v5`, `golang-jwt/v5`, `bcrypt`)
- PostgreSQL 16
- Простой фронт на vanilla JS, отдаётся встраиваемо (`embed`) с бэка

## Быстрый старт

```bash
cp .env.example .env
docker compose up -d       # Postgres
go mod tidy
go run ./cmd/server
```

Фронт: http://localhost:8080
Логин по умолчанию: `admin@example.com` / `admin` (меняется в `.env`).

После входа:

1. **Настройки** — введите Cursor API key и параметры SMTP.
2. **Дайджесты** — создайте дайджест (название, тематика, источники, получатели, период, язык). При сохранении включенного дайджеста обработка запускается сразу.
3. **История** — просмотр ранее составленных дайджестов (в новой вкладке).

## Раскладка

```
cmd/server/             точка входа
internal/
  config/               загрузка env / .env
  db/                   pgxpool + встроенные миграции
  models/               структуры
  repo/                 доступ к БД
  auth/                 JWT access+refresh, bcrypt, middleware
  cursor/               клиент api.cursor.com/v0/agents
  images/               скачивание иллюстраций
  mailer/               отправка SMTP (plain + implicit TLS 465)
  processor/            оркестрация обработки дайджеста + сборка HTML
  scheduler/            ежечасный тик + on-add триггер
  handlers/             HTTP + embed фронта и раздача /images/
```

## Переменные окружения

| Имя | Назначение |
|---|---|
| `HTTP_ADDR` | адрес HTTP (по умолчанию `:8080`) |
| `DATABASE_URL` | строка подключения к Postgres |
| `JWT_SECRET` | секрет для подписи access-токенов |
| `ACCESS_TTL_MIN` | TTL access-токена в минутах |
| `REFRESH_TTL_HOURS` | TTL refresh-токена в часах |
| `INIT_ADMIN_EMAIL` / `INIT_ADMIN_PASSWORD` | учётка, создаваемая при первом запуске, если БД пуста |
| `STORAGE_DIR` | корень локального хранилища (`./storage`) |
| `PUBLIC_BASE_URL` | базовый URL сервиса — для ссылок на картинки в письмах |

## Примечания по Cursor API

Сервис формирует один большой промпт на запуск дайджеста: задаёт тематику, период,
список источников (или разрешает подобрать самостоятельно), ожидает ответ **строго
в JSON** со списком материалов. URL, картинка, `summary_title`, `summary_text`,
полный (при необходимости — переведённый) `full_text`. Если агент подобрал
источники сам, они запоминаются в `auto_sources` у дайджеста. Изображения
скачиваются в `./storage/images/<run_id>/` и раздаются по `/images/...`.
