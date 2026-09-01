# Spec: Go API Gateway (proxy + rate limit + canary) — «простой пример»

> Автор: разбор-гриллинг + to-spec. Трекер: GitHub (`lnkq/agent-test`).
> Один seam тестирования: **HTTP-граница шлюза** (чёрный ящик).

## Problem Statement

Нужен запускаемый API gateway на Go, который читает `config.yml`, проксирует HTTP-запросы на backend-сервисы, умеет ограничивать частоту запросов и делить трафик между версиями сервиса (canary), переживает смену конфига без рестарта. Старт — **локальный самодостаточный пример через docker compose** с наглядным окружением: два демо-апстрима, Prometheus + Grafana и красивый фронт для тестирования запросов. Сложность Kubernetes/поды и распределённого состояния сознательно **отложена**.

## Solution

Собрать полноценный (читаемый, не урезанный ради дедлайна) проект: docker compose поднимает шлюз, два демо-апстрима, Prometheus, Grafana и встроенный в шлюз статический фронт. С фронта можно слать тестовые запросы через шлюз и видеть: какой апстрим ответил, 429 на rate-limit роуте, долю canary-сплита, прямые метрики в Grafana. Смена весов/лимитов в `config.yml` подхватывается горячо, без рестарта процесса.

## User Stories

1. As an operator, I want to define routes in `config.yml` mapping URL prefixes to upstreams, so that I can change routing without recompiling the gateway.
2. As an operator, I want the gateway to hot-reload `config.yml` and atomically apply changes without restarting, so that weight/limit changes take effect in all running instances consistently.
3. As an operator, I want invalid config to be rejected on startup and on reload while keeping the last good config, so that a broken file never takes down routing.
4. As a caller, I want a request to a route to be proxied to the correct upstream service, so that my request reaches the right backend.
5. As a caller, I want the gateway to preserve method, headers and body when proxying, so that upstreams receive semantically identical requests.
6. As an operator, I want each route to point at a weighted list of upstreams, so that I can balance load and run canary rollouts with one mechanism.
7. As an operator, I want to set canary weights (e.g. 80/20) on a route, so that a chosen percentage of traffic goes to the canary version of the service.
8. As a caller, I want to see which upstream answered (via a response header, e.g. `X-Upstream`), so that I can verify the canary split directly.
9. As an operator, I want configurable rate limits per route via named profiles, so that I control per-route request throughput.
10. As a caller, I want to receive HTTP 429 when I exceed a route's rate limit, so that I know I was throttled.
11. As an operator, I want the gateway to health-check upstreams and exclude dead ones from the pool, so that traffic is not sent to a dead service.
12. As a caller, I want a clear HTTP 502 when every upstream of a route is down, so that failures are honest and visible.
13. As an operator, I want a configurable per-route timeout, so that a slow upstream doesn't hang the gateway indefinitely.
14. As an operator, I want the gateway to expose Prometheus metrics (RPS by route, 429 counts, upstream-selection counts, latency histogram, upstream up/status), so that I can monitor it.
15. As an operator, I want a Grafana dashboard showing RPS by route, rate-limit 429s, canary split, latency p50/p95 and upstream up status, so that I can observe behaviour on live data.
16. As a developer, I want a beautiful request-testing frontend (build path/headers/body → send through the gateway → see the response and which upstream answered, plus live 200/429 counters), so that I can validate proxying, canary and rate limiting visually.
17. As a developer, I want a single `docker compose up` to bring up the whole stack (gateway, two upstreams, Prometheus, Grafana), so that the demo runs with one command.
18. As a developer, I want black-box HTTP tests that drive the built gateway binary with an injected config and assert external behaviour (proxying, 429, canary weights, hot-reload), so that the feature set stays correct.
19. As a developer, I want the README to list the manual acceptance checks with expected outputs, so that anyone can verify the example works.

## Implementation Decisions

- **Язык/стек**: Go; маршрутизация и прокси на stdlib (`net/http` + `httputil.ReverseProxy`); YAML через `gopkg.in/yaml.v3`. Минимум внешних зависимостей.
- **Точка входа и пакеты**: один бинарь (`cmd/server`) + `internal`-пакеты: `config`, `router`, `upstream` (пул/health-check/балансировка), `ratelimit`, `proxy`, `metrics`, `frontend`. Разделение по ответственности, без завязок между ними кроме интерфейсов.
- **Конфиг**: `config.yml` содержит роуты, профили rate-limit, пулы апстримов, таймауты. **Hot-reload**: внешняя перезагрузка + атомарный своп неизменяемого (immutable) конфига; при релоаде активного пути. Валидация **fail-closed**: ошибка схемы → старый конфиг остаётся.
- **[из раннего прототипа] Rate limit**: token bucket, лимит на роут, именованные профили:
  ```yaml
  rate_limits:
    standard: { rate: 100, burst: 20 }
  routes:
    - path: /limited
      limit: standard
  ```
  Лимит считается **на инстанс** (на под). Многоинстансность — отложена.
- **Canary/балансировка — один механизм**: роут → упорядоченный список апстримов с весами; выбор — взвешенный случайный (без привязки к клиенту, random split). Ответ помечается заголовком выбранного апстрима.
- **Health-check**: активный TCP/HTTP-ping; исключение из пула после порога подряд-ошибок; повторное включение после успешного проба.
- **Роутинг**: префикс-матчинг пути.
- **Observability**: `/metrics` в формате Prometheus на шлюзе (используя `prometheus/client_golang`); Prometheus грепает шлюз; общий дашборд Grafana (RPS по роуту, 429, canary-сплит, latency p50/p95, up-статус).
- **Фронт**: статическая страница, **встроенная в шлюз** через `embed.FS` (отдельный контейнер не нужен); vanilla JS + лёгкая chart-библиотека, без node-сборки в compose; функционал — конструктор запроса, показ ответа + «какой апстрим», живые счётчики 200/429.
- **Деплой (пример)**: docker compose-сервисы: `gateway`, `upstream-a`, `upstream-b`, `prometheus`, `grafana`. Hot-reload — перечитывание bind-mounted `config.yml` (слежение за файлом/релоад-сигнал).
- **Seam сборочный**: осиновый черный-box тест-харнесс запускает скомпилированный бинарь с инжектированным конфигом и общается с ним по HTTP.

## Testing Decisions

- **Хороший тест** проверяет только **внешнее поведение** на HTTP-границе, не трогая реализацию (как считается bucket, внутреннее хранение маршрутов).
- **Один seam**: чёрный ящик через HTTP — тестовая инфраструктура поднимает скомпилированный бинарь шлюза с тестовым `config.yml` и гоняет настоящие HTTP-запросы через его точку входа.
- **Покрываемые сценарии**: прокси до нужного апстрима (метод/заголовки/тело), 429 на rate-limit роуте после порога, распределение canary по весам (статистически), 502 при полном падении пула, hot-reload (смена весов/лимита подхватывается без рестарта), валидация config (fail-closed).
- Внутренние пакеты (`config`, `ratelimit`, `router`, `upstream`) **без** собственных юнит-тестов в v1 — их правильность проверяется через единственный seam.
- **Prior art**: отсутствует (greenfield) — эталонируется Makefile-таргет `make test` и компоновка тестового харнесса как эталон для будущих тестов.

## Out of Scope

- Многоинстансность / распределённое состояние: глобальные rate-limit счётчики, Redis, sticky-сессии, leader election (в коде — место расширения + TODO).
- Service discovery (K8s/Consul), автоскейл, сам-registration.
- Терминация TLS самим шлюзом (TLS — на инфраструктуре); в примере — plain HTTP.
- authN/authZ, трансформация запросов/ответов, ретраи, circuit breaker, кэширование.
- Привязка canary к клиенту (stickiness) — только random split.
- Метрики самих апстримов (грепаем только шлюз).
- Отдельный контейнер/React-сборка фронта.

## Further Notes

- Многоинстансная вставка (per-pod лимит → глобальный, health-check между подами) помечается TODO на местах расширения, но не реализуется.
- README — 4 ручных акцепт-проверки: прокси, canary-доля, 429 на rate-limit, hot-reload сдвига весов; с ожидаемым выводом (ответ/статус/график).
- Репозиторий greenfield (`.codegraph`/ADRs отсутствуют); кроме этого спека, других артефактов нет.
