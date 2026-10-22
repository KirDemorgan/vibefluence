# Vibefluence

Портал документации для репозиториев GitLab: показывает Markdown-файлы из папок `docs/` деревом, позволяет создавать и править документы из интерфейса и публиковать их в репозиторий через merge request. Для агентов предусмотрен MCP-сервер (в разработке).

Статус: **M0 (фундамент)**. Готовы монорепа, каркас бэкенда и фронтенда, фейковый GitLab, CI. Чтение и запись документов в разработке, план в [docs/02-backlog.md](docs/02-backlog.md).

## Структура

```
backend/    Go: HTTP-сервер, конфиг, фейковый GitLab для тестов
frontend/   Angular 22 + Taiga UI 5
deploy/     compose-файл для запуска образа
docs/       анализ, решения, бэклог, ADR
```

## Требования

- Go 1.27
- Node.js 22.22+ или 24.15+ (проверено на 26.9) и npm
- make

Если `go` не в `PATH` (например, SDK поставила IDE), передайте путь: `make GO=/путь/к/go/bin/go test-backend`.

## Быстрый старт

```bash
make install
make dev
```

`make dev` поднимает три процесса: фейковый GitLab с демо-данными на `:8929`, бэкенд на `:8080` и Angular dev-server на `:4200` с прокси на бэкенд. Откройте http://localhost:4200.

## Команды

| Команда | Что делает |
|---|---|
| `make test` | все тесты (бэкенд с `-race`, фронтенд с покрытием) |
| `make lint` | gofmt, go vet, golangci-lint, ESLint, Prettier |
| `make vuln` | govulncheck и npm audit |
| `make build` | единый бинарник `backend/bin/vibefluence` с встроенным фронтендом |
| `make help` | список всех целей |

`golangci-lint` и `govulncheck` нужно установить отдельно:

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
```

## Запуск

```bash
VIBEFLUENCE_GITLAB_TOKEN=<token> ./backend/bin/vibefluence -config backend/config.example.yaml
```

Конфигурация: YAML ([пример](backend/config.example.yaml)) плюс переменные окружения `VIBEFLUENCE_ADDR`, `VIBEFLUENCE_DATA_DIR`, `VIBEFLUENCE_LOG_LEVEL`, `VIBEFLUENCE_LOG_FORMAT`, `VIBEFLUENCE_GITLAB_BASE_URL`, `VIBEFLUENCE_GITLAB_TOKEN`. Токен задаётся только через окружение. Сервис не имеет аутентификации и рассчитан на внутренний контур.

Запуск в Docker: скопируйте `backend/config.example.yaml` в `deploy/config.yaml`, задайте `VIBEFLUENCE_GITLAB_TOKEN` и выполните `docker compose -f deploy/compose.yaml up --build` (образ в этой среде не собирался).

Эндпоинты: `GET /healthz` (liveness), `GET /readyz` (readiness).

## Документация

- [Анализ конкурентов и реализуемости](docs/01-research.md)
- [Бэклог](docs/02-backlog.md)
- [Решения и дизайн](docs/03-decisions.md)
- [ADR](docs/adr/README.md)
