# Task Service

Сервис для управления задачами с HTTP API на Go.

## Требования

- Go `1.23+`
- Docker и Docker Compose

## Быстрый запуск через Docker Compose

```bash
docker compose up --build
```

После запуска сервис будет доступен по адресу `http://localhost:8080`.

Если `postgres` уже запускался ранее со старой схемой, пересоздай volume:

```bash
docker compose down -v
docker compose up --build
```

Причина в том, что SQL-миграции из каталога `migrations` монтируются в `docker-entrypoint-initdb.d` и применяются только при инициализации пустого data volume.

## Swagger

Swagger UI:

```text
http://localhost:8080/swagger/
```

OpenAPI JSON:

```text
http://localhost:8080/swagger/openapi.json
```

## API

Базовый префикс API:

```text
/api/v1
```

Основные маршруты:

- `POST /api/v1/tasks`
- `GET /api/v1/tasks`
- `GET /api/v1/tasks/{id}`
- `PUT /api/v1/tasks/{id}`
- `DELETE /api/v1/tasks/{id}`

## Периодические задачи

Добавлена поддержка периодичности через отдельную таблицу шаблонов повторения (`task_recurrences`) и фоновый spawner.

Поддерживаемые типы `repetition_type`:

- `daily` — каждый `n`-й день (`{"interval_days": n}`)
- `month_days` — по конкретным числам месяца 1..30 (`{"days": [1, 15, 30]}`)
- `specific_dates` — по конкретным датам (`{"dates": ["2026-05-01", "2026-05-10"]}`)
- `even_month` — по четным числам месяца (`{}`)
- `odd_month` — по нечетным числам месяца (`{}`)

Пример создания периодической задачи:

```json
{
  "title": "Ежедневный обзвон пациентов",
  "description": "Позвонить по списку",
  "status": "new",
  "repetition_type": "daily",
  "repetition_config": {
    "interval_days": 1
  }
}
```

### Как работает spawner

- В `cmd/api/main.go` запускается фоновый процесс спавна (`SPAWNER_INTERVAL`, `SPAWNER_BATCH`).
- Spawner выбирает активные шаблоны с `next_spawn_at <= now`.
- Для каждого шаблона создает недостающие задачи до текущей даты.
- На каждое наступление периода новая задача создается всегда, даже если предыдущая еще `new/in_progress`.
- Идемпотентность обеспечивается уникальным индексом `(recurrence_id, scheduled_for)`.

### Принятые допущения

- Таймзона расчета периодичности — `UTC`.
- Для `specific_dates` разрешены только даты формата `YYYY-MM-DD`.
- Для `specific_dates` при создании требуется минимум одна дата `>=` текущей даты.
- При создании периодической задачи сервис сразу создает первую задачу на ближайшую дату по расписанию (она может быть в будущем).
- Изменение настроек периодичности через `PUT /tasks/{id}` не поддерживается (только при создании).

### Комментарий о выполненной работе

- Добавлена миграция `migrations/0002_add_task_recurrences.up.sql` с новой моделью периодичности и новым типом `specific_dates`.
- Расширен домен задач и репозиторий PostgreSQL для работы с recurrence-шаблонами и сгенерированными задачами.
- Реализован сервис фонового спавна периодических задач.
- Расширены HTTP DTO/обработчики и OpenAPI для передачи `repetition_type` и `repetition_config`.
- Обновлен `docker-compose.yml`: теперь монтируется весь каталог `migrations`, чтобы применялись все SQL-миграции при инициализации БД.
