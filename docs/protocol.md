# Протокол GophKeeper (gRPC API)

Канонический контракт протокола — исходники protobuf в
[`proto/gophkeeper/v1/`](../proto/gophkeeper/v1/): `auth.proto`,
`models.proto`, `service.proto` (с комментариями ко всем полям и RPC).
Этот документ — человекочитаемое описание того же API.

Транспорт: **gRPC поверх HTTP/2**, сериализация protobuf, пакет API
`gophkeeper.v1`. Из proto-файлов генерируется Go-код в
`internal/common/proto/gophkeeperv1` (команда `make generate`, плагины
`protoc-gen-go` / `protoc-gen-go-grpc` через buf).

Swagger/OpenAPI не предоставляется: gRPC-сервисы не имеют OpenAPI-описания «из
коробки» (для этого потребовался бы grpc-gateway/transcoding — вне рамок
проекта). Источником истины являются proto-файлы и этот документ.

## Общие положения

- Все RPC — унарные (request/response), потоковой передачи нет.
- Ошибки передаются стандартными gRPC-статусами (`google.rpc` не используется);
  человекочитаемое сообщение — в `status.message`.
- Время — Unix-секунды UTC (`int64`).

## Аутентификация

Сервис `gophkeeper.v1.AuthService` (`auth.proto`):

| RPC        | Запрос            | Ответ             | Описание                                   |
|------------|-------------------|-------------------|--------------------------------------------|
| `Register` | `RegisterRequest` | `RegisterResponse`| создаёт аккаунт, возвращает пользователя и JWT |
| `Login`    | `LoginRequest`    | `LoginResponse`   | проверяет креды, возвращает пользователя и JWT |

`RegisterRequest`/`LoginRequest`: `login` (string), `password` (string, передаётся
в открытом виде по каналу, на сервере хранится только bcrypt-хэш).
`RegisterResponse`/`LoginResponse`: `user` (см. `User`), `access_token` (JWT).

`Register`/`Login` — единственные RPC без токена.

### Формат заголовка авторизации

Все прочие RPC требуют gRPC-метаданные:

```
authorization: Bearer <access_token>
```

Проверяет серверный interceptor (`internal/server/middleware`): без метаданных,
без заголовка, без префикса `Bearer ` или с просроченным/невалидным JWT запрос
отклоняется `Unauthenticated`. При успехе из токена извлекается `user_id` и
кладётся в контекст запроса; сервисный слой фильтрует данные строго по нему.

## Сервис записей

Сервис `gophkeeper.v1.EntryService` (`service.proto`) управляет записями
авторизованного пользователя:

| RPC      | Запрос                | Ответ                 | Описание                                        |
|----------|-----------------------|-----------------------|-------------------------------------------------|
| `Create` | `CreateEntryRequest`  | `CreateEntryResponse` | сохраняет запись; сервер игнорирует `id`, `version`, таймстампы |
| `Get`    | `GetEntryRequest`     | `GetEntryResponse`    | возвращает запись по `id`                       |
| `List`   | `ListEntriesRequest`  | `ListEntriesResponse` | все записи пользователя (без фильтров и пагинации) |
| `Update` | `UpdateEntryRequest`  | `UpdateEntryResponse` | заменяет содержимое; требует `id` и актуальную `version` (оптимистичная блокировка) |
| `Delete` | `DeleteEntryRequest`  | `DeleteEntryResponse` | удаляет запись по `id`                          |
| `Sync`   | `SyncRequest`         | `SyncResponse`        | полный текущий список записей пользователя (сверка состояния нескольких клиентов) |

### Модель данных

`Entry` (`models.proto`) — центральное сообщение:

| Поле        | Тип         | Описание                                                     |
|-------------|-------------|--------------------------------------------------------------|
| `id`        | string      | серверный идентификатор записи                               |
| `type`      | `EntryType` | интерпретация payload'а (см. ниже)                           |
| `label`     | string      | пользовательское имя записи (не пусто)                       |
| `metadata`  | string      | произвольные текстовые метаданные                            |
| `data`      | bytes       | payload записи (формат зависит от `type`)                    |
| `version`   | int64       | инкрементируется при каждом обновлении; для `Update` обязателен |
| `created_at`| int64       | создание, Unix-секунды UTC                                   |
| `updated_at`| int64       | последняя модификация, Unix-секунды UTC                      |

`EntryType`: `ENTRY_TYPE_UNSPECIFIED` (0, не используется явно),
`ENTRY_TYPE_LOGIN_PASSWORD` (1), `ENTRY_TYPE_TEXT` (2), `ENTRY_TYPE_BINARY` (3),
`ENTRY_TYPE_CARD` (4).

Форматы payload'а (`data`):

| Тип    | Формат                                                            |
|--------|-------------------------------------------------------------------|
| login  | JSON `{"username":"...","password":"..."}`                        |
| card   | JSON `{"number":"...","holder":"...","expiry":"...","cvv":"..."}` |
| text   | UTF-8 текст как есть                                              |
| binary | сырые байты (файл)                                                |

Сервер не интерпретирует payload; семантику типов реализует клиент.

`User` (`models.proto`): `id`, `login`, `created_at`.

## Коды ошибок gRPC

Соответствие ошибок сервисного слоя кодам gRPC
(`internal/server/transport/errors.go`):

| gRPC-код              | Когда возвращается                                                                 |
|-----------------------|-------------------------------------------------------------------------------------|
| `InvalidArgument`     | пустой/слишком длинный логин или label, слишком короткий/длинный пароль, пустые `data`, пустой id записи, неизвестный `EntryType`, `version < 1`, слишком длинные metadata |
| `Unauthenticated`     | нет заголовка `authorization`, невалидный/просроченный JWT (`authentication required`), неверный логин или пароль при `Login` |
| `NotFound`            | запись не найдена (`Get`/`Update`/`Delete`)                                          |
| `AlreadyExists`       | логин уже занят при `Register`                                                       |
| `FailedPrecondition`  | конфликт версий при `Update` (entry изменён другим клиентом; нужно перечитать и повторить) |
| `Internal`            | любая непредвиденная ошибка (детали не раскрываются клиенту)                         |

Также возможны стандартные коды gRPC-инфраструктуры (например, `Unavailable`,
`DeadlineExceeded`).

## Ограничения текущей версии

- Транспорт без TLS: шифрование канала — вне рамок (см. раздел «Безопасность»
  в [README](../README.md)); подключение TLS — план развития.
- `List` возвращает все записи одним ответом: пагинация не реализована
  (`ListEntriesRequest` оставлен под неё).
- Все RPC унарные; инкрементальная синхронизация (`Sync` по дельте) — план
  развития.
