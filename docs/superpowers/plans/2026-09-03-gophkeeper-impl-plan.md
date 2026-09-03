# GophKeeper — План реализации

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Клиент-серверный менеджер паролей GophKeeper с gRPC-протоколом, PostgreSQL-хранилищем, CLI и TUI клиентом.

**Architecture:** Слоистая архитектура (transport → service → repository) на сервере и (cli/tui → service → gateway) на клиенте. Сервер хранит приватные данные с изоляцией по пользователям, аутентификация через JWT.

**Tech Stack:** Go 1.26.5, gRPC + protobuf, PostgreSQL (pgx), JWT (golang-jwt), bcrypt, cobra, bubbletea, golang-migrate, testify.

---

## Принятые архитектурные решения

| Компонент | Решение |
|---|---|
| Протокол | gRPC + protobuf |
| БД сервера | PostgreSQL |
| Шифрование | Нет (изоляция данных по пользователям на уровне БД) |
| Локальный кэш клиента | Нет |
| Аутентификация | JWT токены |
| CLI | cobra |
| TUI | bubbletea |
| Миграции | golang-migrate |
| Тесты | testify |

## Структура проекта

```
gophkeeper/
├── cmd/
│   ├── server/          # Точка входа сервера
│   └── client/          # Точка входа клиента
├── internal/
│   ├── server/
│   │   ├── config/      # Конфигурация сервера
│   │   ├── transport/   # gRPC хэндлеры (парсинг запросов, вызов service, формирование ответов)
│   │   ├── service/     # Бизнес-логика: AuthService, EntryService
│   │   ├── repository/  # Интерфейсы хранилищ
│   │   │   └── postgres/ # PostgreSQL-реализации
│   │   ├── auth/        # JWT генерация/валидация
│   │   ├── middleware/  # gRPC interceptors (auth, logging)
│   │   └── model/       # Доменные модели
│   ├── client/
│   │   ├── config/      # Конфигурация клиента
│   │   ├── transport/   # Cobra команды
│   │   ├── service/     # Бизнес-логика клиента: AuthService, EntryService
│   │   ├── gateway/     # gRPC клиент (реализация интерфейсов, вызываемых service)
│   │   └── tui/         # Bubbletea интерфейс
│   └── common/
│       └── proto/       # Protobuf определения и сгенерированный код
├── migrations/          # SQL миграции
├── docs/
├── proto/               # .proto файлы
└── Makefile
```

---

## Этап 0: Инициализация проекта

**Цель:** Структура каталогов, зависимости, protobuf кодогенерация, Makefile, линтеры.

**Файлы:**
- Create: `go.mod`
- Create: `Makefile`
- Create: `proto/gophkeeper/v1/service.proto`, `proto/gophkeeper/v1/models.proto`, `proto/gophkeeper/v1/auth.proto`
- Create: `buf.gen.yaml`, `buf.yaml`
- Create: `.golangci.yml`
- Create: `Dockerfile.server`

**Задачи:**
- [ ] `go mod init github.com/yourname/gophkeeper`
- [ ] Добавить зависимости: `google.golang.org/grpc`, `google.golang.org/protobuf`, `github.com/golang-migrate/migrate/v4`, `github.com/golang-jwt/jwt/v5`, `github.com/spf13/cobra`, `github.com/charmbracelet/bubbletea`, `github.com/stretchr/testify`, `golang.org/x/crypto`, `github.com/jackc/pgx/v5`
- [ ] Определить protobuf: `AuthService` (Register, Login), `EntryService` (Create, Get, List, Update, Delete, Sync), сообщения для User, Entry (типы: login/password, text, binary, card), Metadata
- [ ] Сгенерировать Go-код из protobuf через `buf generate` или `protoc`
- [ ] Makefile с целями:
  - `lint` — запуск golangci-lint (`golangci-lint run ./...`)
  - `test` — запуск юнит-тестов с покрытием (`go test ./... -race -coverprofile=coverage.out -covermode=atomic` + вывод суммы покрытия)
  - `build` — сборка сервера и клиента (`build-server`, `build-client`) с версийными ldflags
  - `build-server`, `build-client` — сборка бинарников в `bin/`
  - `docker-build` — сборка Docker-образа сервера (`docker build -f Dockerfile.server -t gophkeeper-server .`)
  - `generate` — кодогенерация protobuf
  - `migrate-up`, `migrate-down` — применение/откат миграций
- [ ] Настроить golangci-lint

- [ ] `Dockerfile.server` — многоэтапная сборка серверной части в образ:
```dockerfile
FROM golang:1.26-alpine AS builder
ARG VERSION=dev
ARG BUILD_DATE=unknown
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w -X main.version=${VERSION} -X main.buildDate=${BUILD_DATE}" -o /bin/gophkeeper-server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 1000 appuser
COPY --from=builder /bin/gophkeeper-server /usr/local/bin/
COPY migrations/ /migrations/
USER appuser
EXPOSE 50051
ENTRYPOINT ["gophkeeper-server"]
```
- Образ: минимальный (distroless/alpine), non-root пользователь, миграции включены в образ, версия и дата сборки передаются через build args.

**Коммит:** `feat: init project structure with protobuf definitions`

---

## Этап 1: Миграции и доменные модели

**Цель:** Схема БД, миграции, доменные модели.

**Файлы:**
- Create: `migrations/000001_create_users.up.sql`, `...down.sql`
- Create: `migrations/000002_create_entries.up.sql`, `...down.sql`
- Create: `internal/server/model/user.go`
- Create: `internal/server/model/entry.go`
- Create: `internal/server/model/errors.go`

**Задачи:**

- [ ] Миграция `users`:
```sql
CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    login      VARCHAR(255) UNIQUE NOT NULL,
    pass_hash  VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

- [ ] Миграция `entries`:
```sql
CREATE TABLE entries (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type       SMALLINT NOT NULL,  -- 1=login, 2=text, 3=binary, 4=card
    label      VARCHAR(255) NOT NULL,
    metadata   TEXT,
    data       BYTEA NOT NULL,
    version    BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_entries_user_id ON entries(user_id);
```

- [ ] Доменные модели:
```go
// model/user.go
type User struct {
    ID        string
    Login     string
    PassHash  string
    CreatedAt time.Time
}

// model/entry.go
type EntryType int

const (
    EntryTypeLoginPassword EntryType = 1
    EntryTypeText          EntryType = 2
    EntryTypeBinary        EntryType = 3
    EntryTypeCard          EntryType = 4
)

type Entry struct {
    ID        string
    UserID    string
    Type      EntryType
    Label     string
    Metadata  string
    Data      []byte
    Version   int64
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

- [ ] Кастомные ошибки: `ErrNotFound`, `ErrAlreadyExists`, `ErrConflict`, `ErrUnauthorized`

**Коммит:** `feat: add database migrations and domain models`

---

## Этап 2: Репозитории (storage layer)

**Цель:** Интерфейсы и PostgreSQL-реализации хранилищ с unit-тестами.

**Файлы:**
- Create: `internal/server/repository/user.go` (интерфейс `UserRepository`)
- Create: `internal/server/repository/entry.go` (интерфейс `EntryRepository`)
- Create: `internal/server/repository/postgres/user.go`
- Create: `internal/server/repository/postgres/entry.go`
- Create: `internal/server/repository/postgres/postgres.go` (подключение)
- Test: `internal/server/repository/postgres/user_test.go`
- Test: `internal/server/repository/postgres/entry_test.go`

**Задачи:**

- [ ] Интерфейс `UserRepository`:
```go
type UserRepository interface {
    Create(ctx context.Context, user *model.User) error
    GetByLogin(ctx context.Context, login string) (*model.User, error)
    GetByID(ctx context.Context, id string) (*model.User, error)
}
```

- [ ] Интерфейс `EntryRepository`:
```go
type EntryRepository interface {
    Create(ctx context.Context, entry *model.Entry) error
    GetByID(ctx context.Context, userID, entryID string) (*model.Entry, error)
    List(ctx context.Context, userID string, entryType *model.EntryType) ([]*model.Entry, error)
    Update(ctx context.Context, entry *model.Entry) error
    Delete(ctx context.Context, userID, entryID string) error
}
```

- [ ] Реализация на PostgreSQL через `pgx` (prepared statements, context propagation)
- [ ] Тесты с тестовой БД (testcontainers-pg или отдельная тестовая БД)

**Коммит:** `feat: implement repository layer with PostgreSQL`

---

## Этап 3: Auth service (сервер)

**Цель:** Хэширование паролей, JWT, бизнес-логика регистрации/логина.

**Файлы:**
- Create: `internal/server/service/auth.go`
- Create: `internal/server/auth/jwt.go`
- Create: `internal/server/middleware/auth.go`
- Test: `internal/server/service/auth_test.go`
- Test: `internal/server/auth/jwt_test.go`

**Задачи:**

- [ ] `auth/jwt.go` — генерация и валидация JWT (claims: user_id, login, exp, iat). Конфигурация: secret key, token TTL.

- [ ] `service/auth.go`:
```go
type AuthService struct {
    users repository.UserRepository
    jwt   *auth.JWTManager
}

func (s *AuthService) Register(ctx context.Context, login, password string) (string, error)
func (s *AuthService) Login(ctx context.Context, login, password string) (string, error)
```
- Register: валидация входных данных, проверка уникальности, хэширование bcrypt, создание пользователя, генерация JWT
- Login: поиск по login, проверка bcrypt, генерация JWT

- [ ] `middleware/auth.go` — gRPC unary interceptor, извлекает JWT из metadata (`authorization`), валидирует, кладёт user_id в context

- [ ] Тесты AuthService с моком `UserRepository` (testify/mock)

**Коммит:** `feat: implement auth service with JWT and bcrypt`

---

## Этап 4: Entry service (сервер)

**Цель:** Бизнес-логика CRUD для записей.

**Файлы:**
- Create: `internal/server/service/entry.go`
- Test: `internal/server/service/entry_test.go`

**Задачи:**

- [ ] `service/entry.go`:
```go
type EntryService struct {
    entries repository.EntryRepository
}

func (s *EntryService) Create(ctx context.Context, userID string, entry *model.Entry) (*model.Entry, error)
func (s *EntryService) Get(ctx context.Context, userID, entryID string) (*model.Entry, error)
func (s *EntryService) List(ctx context.Context, userID string, entryType *model.EntryType) ([]*model.Entry, error)
func (s *EntryService) Update(ctx context.Context, userID string, entry *model.Entry) (*model.Entry, error)
func (s *EntryService) Delete(ctx context.Context, userID, entryID string) error
```

- [ ] Валидация: тип записи, обязательные поля, принадлежность записи пользователю
- [ ] Конфликт версий при Update — возвращает `ErrConflict`, если версия не совпадает (optimistic locking)
- [ ] Тесты с моком `EntryRepository`

**Коммит:** `feat: implement entry service with CRUD and conflict resolution`

---

## Этап 5: gRPC хэндлеры (transport layer сервера)

**Цель:** Реализация gRPC сервисов, маппинг protobuf ↔ service.

**Файлы:**
- Create: `internal/server/transport/auth.go`
- Create: `internal/server/transport/entry.go`
- Test: `internal/server/transport/auth_test.go`
- Test: `internal/server/transport/entry_test.go`

**Задачи:**

- [ ] `transport/auth.go` — реализация `pb.AuthServiceServer`:
  - Register: парсинг запроса, вызов `AuthService.Register`, формирование ответа с токеном
  - Login: аналогично

- [ ] `transport/entry.go` — реализация `pb.EntryServiceServer`:
  - Create/Get/List/Update/Delete: извлечение user_id из context (после middleware), вызов service, маппинг в protobuf
  - Обработка ошибок → gRPC status codes (NotFound, AlreadyExists, PermissionDenied, Internal)

- [ ] Конвертеры: `model.Entry` ↔ `pb.Entry`, `model.EntryType` ↔ `pb.EntryType`

- [ ] Тесты: вызов хэндлеров с моками сервисов, проверка gRPC status codes

**Коммит:** `feat: implement gRPC transport handlers`

---

## Этап 6: Сервер — точка входа и конфигурация

**Цель:** Запуск сервера, graceful shutdown, конфигурация.

**Файлы:**
- Create: `cmd/server/main.go`
- Create: `internal/server/config/config.go`

**Задачи:**

- [ ] Конфигурация через флаги + env:
  - `--address` / `GRPC_ADDRESS` (default `:50051`)
  - `--dsn` / `DATABASE_DSN`
  - `--jwt-secret` / `JWT_SECRET`
  - `--jwt-ttl` / `JWT_TTL` (default 24h)

- [ ] `main.go`:
  - Парсинг конфигурации
  - Подключение к PostgreSQL
  - Запуск миграций (golang-migrate)
  - Инициализация repository → service → transport
  - Создание gRPC сервера с middleware
  - Graceful shutdown по SIGINT/SIGTERM

- [ ] Логирование (slog из stdlib)

**Коммит:** `feat: implement server entry point with config and graceful shutdown`

---

## Этап 7: Клиент — gateway (gRPC клиент)

**Цель:** gRPC клиент с управлением JWT токеном.

**Файлы:**
- Create: `internal/client/gateway/gateway.go`
- Create: `internal/client/gateway/auth.go`
- Create: `internal/client/gateway/entry.go`

**Задачи:**

- [ ] `gateway/gateway.go` — создание gRPC connection, автоматическая подстановка JWT в metadata:
```go
type Gateway struct {
    conn    *grpc.ClientConn
    auth    pb.AuthServiceClient
    entries pb.EntryServiceClient
    token   string
}
```

- [ ] Интерфейсы, которые вызывает клиентский service:
```go
type AuthGateway interface {
    Register(ctx context.Context, login, password string) (token string, err error)
    Login(ctx context.Context, login, password string) (token string, err error)
}

type EntryGateway interface {
    Create(ctx context.Context, entry *Entry) (*Entry, error)
    Get(ctx context.Context, id string) (*Entry, error)
    List(ctx context.Context) ([]*Entry, error)
    Update(ctx context.Context, entry *Entry) (*Entry, error)
    Delete(ctx context.Context, id string) error
}
```

- [ ] Хранение токена: в памяти во время сессии + в файле `~/.gophkeeper/token` для персистентности

**Коммит:** `feat: implement client gRPC gateway`

---

## Этап 8: Клиент — service слой

**Цель:** Бизнес-логика клиента.

**Файлы:**
- Create: `internal/client/service/auth.go`
- Create: `internal/client/service/entry.go`
- Test: `internal/client/service/auth_test.go`
- Test: `internal/client/service/entry_test.go`

**Задачи:**

- [ ] `service/auth.go`:
```go
type AuthService struct {
    gateway gateway.AuthGateway
    token   *TokenStore
}

func (s *AuthService) Register(ctx context.Context, login, password string) error
func (s *AuthService) Login(ctx context.Context, login, password string) error
func (s *AuthService) Logout() error
func (s *AuthService) IsAuthenticated() bool
```

- [ ] `service/entry.go`:
```go
type EntryService struct {
    gateway gateway.EntryGateway
}

func (s *EntryService) Add(ctx context.Context, entry *Entry) (*Entry, error)
func (s *EntryService) List(ctx context.Context) ([]*Entry, error)
func (s *EntryService) Get(ctx context.Context, id string) (*Entry, error)
func (s *EntryService) Edit(ctx context.Context, entry *Entry) (*Entry, error)
func (s *EntryService) Remove(ctx context.Context, id string) error
```

- [ ] Тесты с моками gateway

**Коммит:** `feat: implement client service layer`

---

## Этап 9: Клиент — CLI команды (cobra)

**Цель:** Пользовательский CLI-интерфейс.

**Файлы:**
- Create: `cmd/client/main.go`
- Create: `internal/client/cli/root.go`
- Create: `internal/client/cli/register.go`
- Create: `internal/client/cli/login.go`
- Create: `internal/client/cli/logout.go`
- Create: `internal/client/cli/add.go`
- Create: `internal/client/cli/list.go`
- Create: `internal/client/cli/get.go`
- Create: `internal/client/cli/edit.go`
- Create: `internal/client/cli/delete.go`
- Create: `internal/client/cli/version.go`
- Create: `internal/client/config/config.go`

**Задачи:**

- [ ] Корневая команда + подкоманды:
  - `gophkeeper register --login=<login> --password=<pass>`
  - `gophkeeper login --login=<login> --password=<pass>`
  - `gophkeeper logout`
  - `gophkeeper add --type=<login|text|binary|card> --label=<label> [--metadata=<meta>] [data flags по типу]`
  - `gophkeeper list [--type=<type>]`
  - `gophkeeper get --id=<id>`
  - `gophkeeper edit --id=<id> [flags...]`
  - `gophkeeper delete --id=<id>`
  - `gophkeeper version`
  - `gophkeeper tui` — запуск интерактивного режима

- [ ] Флаги для типов данных:
  - login/password: `--username`, `--password`
  - text: `--text`
  - binary: `--file=<path>`
  - card: `--number`, `--holder`, `--expiry`, `--cvv`

- [ ] Конфигурация клиента: `--server` / `SERVER_ADDRESS` (default `localhost:50051`)

- [ ] Информация о версии: `ldflags` при сборке (`-X main.version=... -X main.buildDate=...`)

- [ ] Вывод в консоль: таблица для list, форматированный вывод для get

**Коммит:** `feat: implement CLI commands with cobra`

---

## Этап 10: Клиент — TUI (bubbletea)

**Цель:** Интерактивный терминальный интерфейс.

**Файлы:**
- Create: `internal/client/tui/app.go`
- Create: `internal/client/tui/list.go`
- Create: `internal/client/tui/detail.go`
- Create: `internal/client/tui/form.go`
- Create: `internal/client/tui/styles.go`

**Задачи:**

- [ ] `tui/app.go` — главная модель bubbletea, навигация между экранами
- [ ] Экраны:
  - Список записей (с фильтрацией по типу)
  - Детальный просмотр записи
  - Форма создания/редактирования
  - Подтверждение удаления
- [ ] Горячие клавиши: `n` — new, `e` — edit, `d` — delete, `enter` — view, `q` — quit, `/` — filter
- [ ] Стилизация через lipgloss

**Коммит:** `feat: implement TUI with bubbletea`

---

## Этап 11: Документация и тесты

**Цель:** Покрытие тестами ≥70%, godoc, Swagger.

**Файлы:**
- Create: `docs/api.swagger.yaml` (через grpc-gateway или вручную)
- Modify: все пакеты — добавить package doc comments

**Задачи:**

- [ ] Проверить покрытие: `go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out`
- [ ] Добить до ≥70% интеграционными тестами (полный цикл через gRPC на тестовой БД)
- [ ] godoc-комментарии ко всем экспортированным символам
- [ ] Swagger/OpenAPI описание через grpc-gateway (опционально)
- [ ] Обновить README.md: описание, инструкция по запуску, примеры использования

**Коммит:** `docs: add documentation, tests coverage >=70%`

---

## Порядок выполнения

```
Этап 0 → Этап 1 → Этап 2 → Этап 3 → Этап 4 → Этап 5 → Этап 6
                                                          ↓
                                          Сервер готов к запуску
                                                          ↓
                               Этап 7 → Этап 8 → Этап 9 → Этап 10 → Этап 11
```

Этапы 0–6 — сервер, 7–10 — клиент, 11 — финализация. После Этапа 6 сервер полностью функционален и может быть протестирован через grpcurl.

---

## Этап 12: Финальная верификация соответствия ТЗ

**Цель:** Убедиться, что все обязательные пункты ТЗ выполнены.

**Задачи:**

- [ ] **Сервер — бизнес-логика:**
  - [ ] Регистрация пользователя работает (`gophkeeper register`)
  - [ ] Аутентификация работает (`gophkeeper login`), неверный пароль отклоняется
  - [ ] Авторизация: незалогиненный клиент не может получить доступ к данным; пользователь A не имеет доступа к данным пользователя B
  - [ ] Хранение приватных данных всех 4 типов: логин/пароль, текст, бинарные данные, банковские карты
  - [ ] Произвольная текстовая метаинформация хранится для любого типа данных
  - [ ] Синхронизация между несколькими клиентами одного владельца: изменения, внесённые через один клиент, видны другому после синхронизации
  - [ ] Передача данных владельцу по запросу (`gophkeeper get`, `gophkeeper list`)

- [ ] **Клиент — бизнес-логика:**
  - [ ] Аутентификация и авторизация на удалённом сервере
  - [ ] Доступ к приватным данным по запросу

- [ ] **Дополнительные требования:**
  - [ ] Клиент собирается под Windows, Linux, macOS (проверить `GOOS=windows go build`, `GOOS=linux go build`, `GOOS=darwin go build`)
  - [ ] `gophkeeper version` выводит версию и дату сборки бинарника

- [ ] **Тестирование и документация:**
  - [ ] Покрытие юнит-тестами ≥70%: `go tool cover -func=coverage.out | tail -1`
  - [ ] Каждая экспортированная функция, тип, переменная и пакет имеют godoc-комментарии (проверить `go vet`, просмотр `go doc ./...`)
  - [ ] `make lint` проходит без ошибок
  - [ ] `make test` проходит без ошибок

- [ ] **Сборка и развёртывание:**
  - [ ] `make docker-build` собирает образ сервера
  - [ ] Сервер запускается из образа и проходит smoke-тест (register → login → add → list через клиент)

- [ ] **Необязательные функции (реализованные):**
  - [ ] TUI работает (`gophkeeper tui`)
  - [ ] Бинарный протокол (gRPC)
  - [ ] Swagger-описание протокола присутствует в `docs/`

По каждому пункту: выполнить фактическую проверку командой/сценарием, зафиксировать результат. Любое несоответствие — вернуть в соответствующий этап и исправить.

**Коммит:** `chore: verify TZ compliance`
