# GophKeeper

GophKeeper — клиент-серверный менеджер приватных данных. Сервер хранит данные
зарегистрированных пользователей в PostgreSQL, клиент — консольное приложение
(CLI + TUI) для работы с хранилищем.

Поддерживаемые типы данных (записей):

- **login** — пара «логин/пароль» (учётные данные);
- **text** — произвольный текст (заметка);
- **binary** — произвольные двоичные данные (например, файл);
- **card** — данные банковской карты (номер, владелец, срок, CVV).

Каждая запись имеет метку (label), произвольные текстовые метаданные и версию.
Версия используется для оптимистичной блокировки: при обновлении на основе
устаревшей версии сервер отвечает ошибкой конфликта. Данные одного пользователя
изолированы от данных других.

## Архитектура

Клиент и сервер общаются по **gRPC** (HTTP/2, protobuf). Контракт API задан
proto-файлами в [`proto/gophkeeper/v1/`](proto/gophkeeper/v1/); человекочитаемое
описание протокола — [docs/protocol.md](docs/protocol.md).

Аутентификация: регистрация/логин возвращают **JWT**-токен доступа; все
остальные RPC требуют заголовок gRPC-метаданных `authorization: Bearer <token>`
(проверяется серверным interceptor'ом). Пароли на сервере хранятся только в
виде bcrypt-хэша.

Сервер многослойный:

```
gRPC (transport) → middleware (auth) → service (валидация, бизнес-логика)
                                        → repository (интерфейсы)
                                          → postgres (реализация, pgx)
```

Миграции схемы БД встроены в бинарник сервера (`migrations`, golang-migrate +
embed) и применяются при старте.

### Структура проекта

```
gophkeeper/
├── bin/                          # собранные бинарники (make build)
├── cmd/
│   ├── client/                   # точка входа клиента
│   └── server/                   # точка входа сервера
├── docs/
│   ├── protocol.md               # описание gRPC-протокола
│   └── superpowers/plans/        # рабочие планы разработки
├── internal/
│   ├── client/
│   │   ├── cli/                  # cobra-команды (register, login, add, ...)
│   │   ├── config/               # конфигурация клиента
│   │   ├── gateway/              # gRPC-клиент, преобразование типов
│   │   ├── model/                # клиентские типы данных
│   │   ├── render/               # форматирование вывода (таблица, карточка)
│   │   ├── service/              # сервисный слой клиента
│   │   ├── token/                # хранение токена (~/.gophkeeper/token, 0600)
│   │   └── tui/                  # интерактивный TUI (bubbletea)
│   ├── common/proto/gophkeeperv1 # сгенерированный protobuf/gRPC-код
│   └── server/
│       ├── auth/                 # JWT: выпуск и проверка токенов
│       ├── config/               # конфигурация сервера (флаги + env)
│       ├── middleware/           # gRPC interceptor аутентификации
│       ├── model/                # серверные типы и ошибки
│       ├── repository/           # интерфейсы хранилища
│       │   └── postgres/         # реализация на pgx
│       ├── service/              # бизнес-логика, валидация
│       └── transport/            # реализация gRPC-сервисов
├── migrations/                   # SQL-миграции (встраиваются в сервер)
├── proto/gophkeeper/v1/          # исходники protobuf (контракт API)
├── buf.gen.yaml, buf.yaml        # конфигурация buf
├── Dockerfile.server             # образ сервера
└── Makefile
```

## Требования

- **Go 1.26** — сборка и запуск из исходников;
- **Docker** — для локальной базы PostgreSQL, сборки образа сервера,
  а также для интеграционных тестов (testcontainers);
- **golangci-lint** — линтер (`make lint`);
- **buf** — только для регенерации gRPC-кода из proto-файлов (`make generate`),
  для сборки и запуска не нужен.

## Быстрый старт: сервер

### Через Docker

```bash
# собрать образ
make docker-build

# запустить вместе с PostgreSQL
docker network create gophkeeper
docker run -d --name gophkeeper-db --network gophkeeper \
  -e POSTGRES_USER=gophkeeper -e POSTGRES_PASSWORD=gophkeeper \
  -e POSTGRES_DB=gophkeeper postgres:17

docker run -d --name gophkeeper-server --network gophkeeper -p 50051:50051 \
  -e DATABASE_DSN='postgres://gophkeeper:gophkeeper@gophkeeper-db:5432/gophkeeper?sslmode=disable' \
  -e JWT_SECRET='<секрет для подписи токенов>' \
  gophkeeper-server
```

Обязательные переменные окружения: `DATABASE_DSN` и `JWT_SECRET` (см.
[Конфигурация](#конфигурация)). Миграции применяются автоматически при старте
контейнера.

### Из исходников

```bash
# 1. Поднять PostgreSQL (например, в Docker)
docker run -d --name gophkeeper-db -p 5432:5432 \
  -e POSTGRES_USER=gophkeeper -e POSTGRES_PASSWORD=gophkeeper \
  -e POSTGRES_DB=gophkeeper postgres:17

# 2. Собрать и запустить сервер
make build
DATABASE_DSN='postgres://gophkeeper:gophkeeper@localhost:5432/gophkeeper?sslmode=disable' \
JWT_SECRET='dev-secret' \
./bin/gophkeeper-server
```

Ручное управление миграциями (опционально, сервер делает это сам):

```bash
make migrate-up   # применить все
make migrate-down # откатить последнюю
```

## Быстрый старт: клиент

```bash
make build
./bin/gophkeeper-client --help
```

### Дистрибутивы клиента для разных платформ

Кросс-сборка клиента под Windows, Linux и macOS (amd64 и arm64) — по требованию ТЗ
клиент распространяется в виде CLI-приложения для этих платформ:

```bash
make build-client-all
```

Результат — статические бинарники в `bin/` (CGO отключён, внешних зависимостей нет):

```
bin/gophkeeper-client-windows-amd64.exe
bin/gophkeeper-client-windows-arm64.exe
bin/gophkeeper-client-linux-amd64
bin/gophkeeper-client-linux-arm64
bin/gophkeeper-client-darwin-amd64
bin/gophkeeper-client-darwin-arm64
```

Каждый бинарник содержит зашитые при сборке версию и дату сборки
(`gophkeeper-client-<os>-<arch> version`).

Примеры (адрес сервера по умолчанию `localhost:50051`):

```bash
# Регистрация: пароль будет запрошен интерактивно без эха
./bin/gophkeeper-client register --login=alice

# Вход / выход
./bin/gophkeeper-client login --login=alice
./bin/gophkeeper-client logout

# Добавление записей разных типов
./bin/gophkeeper-client add --type=login --label='GitHub' \
  --username=alice --password='s3cret' --metadata='рабочий аккаунт'
./bin/gophkeeper-client add --type=text --label='Заметка' --text='seed-фраза …'
./bin/gophkeeper-client add --type=binary --label='Бэкап' --file=/path/to/file.bin
./bin/gophkeeper-client add --type=card --label='Карта' \
  --number='4111111111111111' --holder='ALICE SMITH' --expiry='12/28' --cvv='123'

# Просмотр и изменение
./bin/gophkeeper-client list                       # таблица записей
./bin/gophkeeper-client list --type=card           # только карты
./bin/gophkeeper-client get --id=<id или префикс>  # показать запись
./bin/gophkeeper-client edit --id=<id> --label='Новое имя'
./bin/gophkeeper-client delete --id=<id>           # с подтверждением
./bin/gophkeeper-client delete --id=<id> --yes     # без подтверждения

# Полная синхронизация состояния с сервером
./bin/gophkeeper-client sync

# Интерактивный TUI
./bin/gophkeeper-client tui

# Версия и сборка
./bin/gophkeeper-client version
```

`get` показывает payload в зависимости от типа: у карты номер маскируется до
последних 4 цифр, CVV скрыт полностью; у binary-записи показывается только
размер. `list` отображает первые 8 символов id; `get`/`edit`/`delete`
принимают полный id или уникальный префикс.

## Форматы хранимых данных

Запись на проводе — protobuf-сообщение `gophkeeper.v1.Entry`: `id`, `type`,
`label`, `metadata`, `data` (payload в байтах), `version`, `created_at`,
`updated_at`. Значение поля `data` зависит от `type`:

| Тип    | Payload (`data`)                                                                 |
|--------|----------------------------------------------------------------------------------|
| login  | JSON `{"username":"...","password":"..."}`                                        |
| card   | JSON `{"number":"...","holder":"...","expiry":"...","cvv":"..."}`                 |
| text   | UTF-8 текст как есть                                                              |
| binary | сырые байты файла                                                                 |

Поля JSON хранятся в открытом виде внутри `data`; сервер не интерпретирует
payload, типозависимую семантику реализует клиент (валидация, маскирование при
выводе).

## Конфигурация

Приоритет источников: **флаг командной строки > переменная окружения > значение
по умолчанию**.

### Сервер (`gophkeeper-server`)

| Флаг           | Env             | По умолчанию | Описание                                            |
|----------------|-----------------|--------------|-----------------------------------------------------|
| `--address`    | `GRPC_ADDRESS`  | `:50051`     | адрес gRPC-сервера (host:port)                      |
| `--dsn`        | `DATABASE_DSN`  | — (обязателен)| строка подключения к PostgreSQL в виде `postgres://` URL |
| `--jwt-secret` | `JWT_SECRET`    | — (обязателен)| секрет HMAC для подписи JWT                          |
| `--jwt-ttl`    | `JWT_TTL`       | `24h`        | время жизни токена (Go duration)                    |
| `--log-level`  | `LOG_LEVEL`     | `info`       | уровень логов: `debug`, `info`, `warn`, `error`     |

### Клиент (`gophkeeper-client`)

| Флаг            | Env                    | По умолчанию          | Описание              |
|-----------------|------------------------|-----------------------|-----------------------|
| `--server`      | `SERVER_ADDRESS`       | `localhost:50051`     | адрес сервера host:port |
| `--token-path`  | `GOPHKEEPER_TOKEN_PATH`| `~/.gophkeeper/token` | путь к файлу токена   |

Токен сохраняется в файл с правами `0600` атомарной записью.

## Разработка

Основные make-цели:

| Цель                          | Действие                                                        |
|-------------------------------|-----------------------------------------------------------------|
| `make lint`                   | golangci-lint                                                   |
| `make test`                   | все тесты с `-race` и профилем покрытия                         |
| `make build`                  | сборка `bin/gophkeeper-server` и `bin/gophkeeper-client`        |
| `make build-client-all`       | кросс-сборка клиента для всех платформ (см. ниже)               |
| `make docker-build`           | образ сервера `gophkeeper-server`                               |
| `make generate`               | регенерация gRPC-кода из `proto/` (нужен buf + плагины)         |
| `make migrate-up/down`        | миграции (нужен `migrate` CLI; DSN переопределяется `DSN=...`)  |
| `make all`                    | lint + test + build                                             |

Тесты и покрытие:

```bash
make test
go tool cover -func=coverage.out   # покрытие по функциям
go tool cover -html=coverage.out   # покрытие в браузере
```

Интеграционные тесты репозитория и транспорта используют testcontainers и
требуют запущенного Docker. Если тестовая база уже есть, её можно подсунуть
через переменную `TEST_DATABASE_DSN`, чтобы не поднимать контейнер.

Суммарное покрытие кода тестами превышает целевые 70% по ТЗ; актуальное
значение выводит `make test`. Покрытие измеряется по всем пакетам, включая
сгенерированный protobuf-код; пакет `internal/common/proto` покрыт контрактными
тестами (round-trip сообщений и проверка RPC-обвязки поверх bufconn).

Swagger/OpenAPI не предоставляется: сетевой протокол — gRPC поверх HTTP/2,
канонический контракт — исходники protobuf в `proto/gophkeeper/v1/` (с
комментариями ко всем полям и RPC) и описание в [docs/protocol.md](docs/protocol.md).

## Безопасность

Реализовано:

- изоляция данных пользователей: каждый запрос авторизуется по JWT, доступ к
  записям возможен только к своим;
- пароли хранятся только как bcrypt-хэш; ограничения на длину пароля с обеих
  сторон;
- JWT с настраиваемым TTL (`JWT_TTL`);
- файл токена клиента — права `0600`, атомарная запись;
- интерактивный ввод пароля без эха (флаг `--password` не обязателен);
- маскирование чувствительных значений в выводе (`get`, `tui`) и в логах
  конфигурации сервера (DSN с паролем печатается отредактированным).

Осознанно вне рамок текущей версии:

- **TLS между клиентом и сервером отсутствует** — транспорт не шифруется и
  рассчитан на защищённый контур (localhost/VPN/service mesh). Подключение TLS
  — следующий шаг развития;
- **клиентское шифрование payload отсутствует** (по решению ТЗ): данные
  передаются и хранятся сервером в виде, определённом выше; защита данных
  на сервере — средствами изоляции пользователей и инфраструктуры БД.

## Известные ограничения

- размер одной записи ограничен лимитом gRPC-сообщения в 4 МиБ (умолчание
  gRPC): binary-запись крупнее ~4 МиБ будет отклонена с ошибкой
  `ResourceExhausted`;
- защита от перебора пароля на `Login` не реализована (rate limiting
  отсутствует): сдерживание подбора — на сетевом уровне (fail2ban, WAF и т.п.).
