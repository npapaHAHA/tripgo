# Trip Service

Сервис управляет поездками: создаёт поездку, возвращает её состояние и завершает
её. Данные хранятся в PostgreSQL, HTTP API реализован по OpenAPI-контракту.

## Требования

- Go 1.27.1;
- Docker Desktop с интеграцией WSL 2;
- Ubuntu 24.04 в WSL 2;
- `tripgoctl`;
- `make`.

## Локальный запуск

Из корня репозитория:

```bash
tripgoctl cluster start
tripgoctl environment start
tripgoctl connect
make migrate
make run
```

`tripgoctl connect` создаёт локальный `.env` с адресом PostgreSQL.
`make run` загружает безопасные значения из `.env.example`, а затем перекрывает
их значениями из `.env`.

Проверка состояния сервиса:

```bash
curl -i localhost:8080/health
curl -i localhost:8080/ready
```

## HTTP API

| Метод | Путь | Назначение |
|---|---|---|
| `POST` | `/api/v1/trips` | создать поездку |
| `GET` | `/api/v1/trips/{tripId}` | получить поездку |
| `POST` | `/api/v1/trips/{tripId}/finish` | завершить поездку |
| `GET` | `/health` | проверить, что процесс работает |
| `GET` | `/ready` | проверить доступность PostgreSQL |

Пример создания:

```bash
curl -sS -X POST localhost:8080/api/v1/trips \
  -H 'Content-Type: application/json' \
  -d '{
    "user_id": "5cb72c04-7650-45c9-a79b-bcdba0631e0c",
    "driver_id": "8860b315-ec86-42eb-a17c-7c163d721ff5",
    "start_point": {"latitude": 59.9398, "longitude": 30.3146},
    "end_point": {"latitude": 59.9290, "longitude": 30.3626},
    "price": 1450
  }'
```

Ошибки возвращаются в формате `application/problem+json` со стабильным полем
`code`.

## Конфигурация

Все настройки приходят из переменных окружения.

| Переменная | Назначение | Пример |
|---|---|---|
| `HTTP_ADDR` | адрес HTTP-сервера | `:8080` |
| `HTTP_READ_TIMEOUT` | таймаут чтения запроса | `10s` |
| `HTTP_READ_HEADER_TIMEOUT` | таймаут чтения заголовков | `5s` |
| `HTTP_WRITE_TIMEOUT` | таймаут записи ответа | `15s` |
| `HTTP_IDLE_TIMEOUT` | таймаут keep-alive соединения | `60s` |
| `LOG_LEVEL` | уровень логирования | `info` |
| `SHUTDOWN_TIMEOUT` | общий бюджет graceful shutdown | `10s` |
| `DATABASE_URL` | строка подключения PostgreSQL | `postgres://...` |
| `DATABASE_MAX_CONNS` | максимальный размер пула | `10` |
| `DATABASE_MIN_CONNS` | минимальный размер пула | `2` |
| `DATABASE_MAX_CONN_LIFETIME` | время жизни соединения | `30m` |
| `DATABASE_CONNECT_TIMEOUT` | таймаут подключения | `5s` |
| `DATABASE_QUERY_TIMEOUT` | таймаут операции с БД | `3s` |

Секреты и локальный `.env` не коммитятся.

## Команды

```bash
make generate        # пересоздать Go-код по OpenAPI
make migrate         # применить миграции
make migrate-down    # откатить одну миграцию
make migrate-status  # показать состояние миграций
make run             # запустить сервис
make test            # запустить тесты
```

## Принятые решения

### Транзакции

`TxManager.Do` открывает транзакцию и кладёт её в `context.Context`.
Репозиторий выбирает исполнителя запросов из контекста: транзакцию, если она
есть, или обычный пул. Вложенный `Do` переиспользует текущую транзакцию.

При успешном завершении функции выполняется `COMMIT`, при ошибке или панике —
`ROLLBACK`. Создание и завершение поездки записывают основную строку и историю
статуса атомарно.

Используется уровень изоляции `Read Committed`. Для текущих операций он
достаточен: конкурентное создание защищено уникальным индексом, а конкурентное
завершение выполняется одним `UPDATE` с условием `status = 'active'`.

### Одна активная поездка водителя

Правило закреплено частичным уникальным индексом:

```sql
CREATE UNIQUE INDEX trips_driver_active_uidx
    ON trips (driver_id)
    WHERE status = 'active';
```

Поэтому при конкурентном создании PostgreSQL принимает только одну активную
поездку. Ошибка PostgreSQL `23505` для этого индекса преобразуется в доменную
ошибку `driver_busy` и HTTP `409`.

### Конкурентное завершение

Завершение выполняется атомарным запросом:

```sql
UPDATE trips
SET status = 'completed', finished_at = $1
WHERE id = $2 AND status = 'active';
```

При двух одновременных запросах только один изменяет строку. Второй получает
`trip_completed`, а `finished_at` не перезаписывается.

### API-first

Типы, серверный интерфейс и маршруты генерируются из
`contracts/openapi/trip-service.openapi.yaml`. Сгенерированный файл не
редактируется вручную и коммитится вместе с исходным контрактом.
