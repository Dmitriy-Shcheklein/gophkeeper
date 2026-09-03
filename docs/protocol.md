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

- Основные RPC — унарные (request/response); для больших бинарных payload'ов
  есть потоковые `Upload` (client-streaming) и `DownloadEntryData`
  (server-streaming), см. ниже.
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
| `Get`    | `GetEntryRequest`     | `GetEntryResponse`    | возвращает запись по `id` (для chunked-записей payload не переносится) |
| `List`   | `ListEntriesRequest`  | `ListEntriesResponse` | все записи пользователя; `include_data` (по умолчанию `false`) управляет переносом payload'ов; порядок — по `created_at, id` |
| `Update` | `UpdateEntryRequest`  | `UpdateEntryResponse` | заменяет содержимое; требует `id` и актуальную `version` (оптимистичная блокировка) |
| `Delete` | `DeleteEntryRequest`  | `DeleteEntryResponse` | удаляет запись по `id`                          |
| `Sync`   | `SyncRequest`         | `SyncResponse`        | полный текущий список записей пользователя (`include_data` — как у `List`) |
| `Upload` | stream `UploadEntryRequest` | `UploadEntryResponse` | потоковая загрузка записи чанками (см. ниже) |
| `DownloadEntryData` | `DownloadEntryDataRequest` | stream `DownloadEntryDataResponse` | потоковое скачивание payload'а записи (см. ниже) |

### Потоковая загрузка и скачивание больших payload'ов

Записи с большим payload'ом (файлы) хранятся на сервере чанками (таблица
`entry_chunks`, размер чанка — 1 МиБ) и передаются потоково:

- **`Upload`** (client-streaming): первое сообщение — `header`
  (`entry` без `data` + `expected_version`: 0 — создание, >0 — обновление
  с оптимистичной блокировкой), затем сообщения `chunk`, последнее —
  `footer` с hex-дайджестом SHA-256 полного payload'а. Сервер проверяет
  дайджест и размер; загрузка атомарна — запись становится видимой только
  после успешного завершения потока, прерванная загрузка откатывается.
- **`DownloadEntryData`** (server-streaming): первое сообщение — `header`
  с общим размером, затем `chunk`-сообщения до конца потока.
- `List`/`Sync` по умолчанию **не переносят payload'ы**: записи содержат
  метаданные и `data_size`, контент скачивается по требованию через
  `DownloadEntryData`. Для chunked-записей `Get` также возвращает пустой
  `data` и заполненный `data_size`.
- Максимальный размер payload'а настраивается сервером (`MAX_DATA_SIZE`
  / `--max-data-size`, по умолчанию 1 ГиБ); превышение — `ResourceExhausted`.

### Модель данных

`Entry` (`models.proto`) — центральное сообщение:

| Поле        | Тип         | Описание                                                     |
|-------------|-------------|--------------------------------------------------------------|
| `id`        | string      | серверный идентификатор записи                               |
| `type`      | `EntryType` | интерпретация payload'а (см. ниже)                           |
| `label`     | string      | пользовательское имя записи (не пусто)                       |
| `metadata`  | string      | произвольные текстовые метаданные                            |
| `data`      | bytes       | payload записи (формат зависит от `type`); пуст для chunked-записей |
| `data_size` | int64       | общий размер payload'а в байтах; заполняется даже когда `data` не переносится |
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
| `InvalidArgument`     | пустой/слишком длинный логин или label, слишком короткий/длинный пароль, пустые `data`, пустой id записи, неизвестный `EntryType`, `version < 1`, слишком длинные metadata, чанк больше 1 МиБ, несовпадение SHA-256 в `Upload`, пустой потоковый payload |
| `Unauthenticated`     | нет заголовка `authorization`, невалидный/просроченный JWT (`authentication required`), неверный логин или пароль при `Login` |
| `NotFound`            | запись не найдена (`Get`/`Update`/`Delete`/`DownloadEntryData`)                      |
| `AlreadyExists`       | логин уже занят при `Register`                                                       |
| `FailedPrecondition`  | конфликт версий при `Update`/`Upload` (entry изменён другим клиентом; нужно перечитать и повторить) |
| `ResourceExhausted`   | payload превышает `MAX_DATA_SIZE` сервера                                            |
| `Internal`            | любая непредвиденная ошибка (детали не раскрываются клиенту)                         |

Также возможны стандартные коды gRPC-инфраструктуры (например, `Unavailable`,
`DeadlineExceeded`).

## Ограничения текущей версии

- Транспорт без TLS: шифрование канала — вне рамок (см. раздел «Безопасность»
  в [README](../README.md)); подключение TLS — план развития.
- `List` возвращает все записи одним ответом (payload'ы — только с
  `include_data`): пагинация не реализована (`ListEntriesRequest` оставлен
  под неё).
- Инкрементальная синхронизация (`Sync` по дельте) — план развития.
