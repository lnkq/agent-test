# Gateway

A small, readable API gateway written in Go. It reads a YAML config, proxies
HTTP requests to weighted upstreams, rate-limits per route, and canarys traffic
between versions of a service — all behind one command via docker compose, with
Prometheus + Grafana for observability and an embedded UI for testing requests.

**Status:** simple runnable example (Kubernetes/multi-pod distribution is out
of scope for now — see [Features](#features)).

## Features

- **Proxying & routing** — prefix-based routes to upstreams, per-route timeout,
  `502` when an upstream is unreachable.
- **Canary & weighted balancing** — one weighted-random-split mechanism serves
  both load balancing and canary. The chosen upstream is reported in the
  `X-Upstream` response header.
- **Active health-check** — TCP probe excludes a dead upstream from the pool
  after consecutive failures and re-adds it on recovery.
- **Rate limiting** — token bucket, configured as named profiles per route;
  returns `429` when throttled (per gateway instance).
- **Hot-reload** — `config.yml` changes apply without restarting (fail-closed:
  a broken config keeps the previous one).
- **Observability** — Prometheus `/metrics` on the gateway and a Grafana
  dashboard (RPS, 429s, canary split, latency p50/p95, upstream up/status).
- **Embedded test UI** — build a request, send it through the gateway, see the
  response, the chosen upstream, live 200/429 counters and a canary chart.

Runs on the Go standard library (`net/http` + `httputil.ReverseProxy`).

## Quick start

```bash
docker compose up --build
```

Then open:

| What            | URL                                |
|-----------------|------------------------------------|
| Gateway test UI | http://localhost:8081/ui           |
| Grafana         | http://localhost:3000 (admin/admin)|
| Prometheus      | http://localhost:9090              |

The demo stack runs two upstream services (`upstream-a`, `upstream-b`), each
answering with its own name so you can see routing and canary by eye.

## Manual acceptance checks

1. **Proxying** — open the UI (`/ui`) and send a request to
   `GET /svc-a/ping`. The response body reports `"service": "upstream-a"`.
   Sending to `/svc-b/ping` reports `upstream-b`.
2. **Canary share** — send ~20 requests to `/canary/x` (use
   `curl http://localhost:8081/canary/x`). Roughly 80% respond from
   `upstream-a` and 20% from `upstream-b` (config weights 80/20). Each response
   carries `X-Upstream`. The UI's "Canary split" chart shows the same split.
3. **Rate limiting** — `for i in $(seq 1 8); do curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8081/limited/x; done`.
   The first requests return `200`; once the burst (2) is spent you start to see
   `429` until tokens refill.
4. **Hot-reload** — while the stack runs, edit `config.yml` (e.g. change the
   `/canary` weights to `50/50`) and save. The gateway picks the change up
   automatically — no restart, no `docker compose` command. Re-run the canary
   requests and the split shifts to ~50/50. A deliberately broken `config.yml`
   is ignored (fail-closed) and the previous config keeps serving.

Watch the **Grafana** dashboard (http://localhost:3000) while running the
checks to see RPS, 429s, the canary split and latency live.

## Development

```bash
make build   # build ./bin/gateway and ./bin/upstream
make test    # black-box HTTP test suite (single project seam)
make up      # docker compose up --build -d
make down    # docker compose down
```

The test suite drives the real gateway binary over HTTP with an injected config
(`e2e/` + `internal/blackbox`). Only external behaviour is asserted.

## Configuration

See [`config.yml`](config.yml). Routes map a URL prefix to a weighted list of
upstreams, an optional named rate-limit `limit`, and an optional `timeout`:

```yaml
rate_limits:
  standard: { rate: 5, burst: 2 }

routes:
  - path: /canary
    upstreams:
      - url: http://upstream-a:8081
        weight: 80
      - url: http://upstream-b:8082
        weight: 20
```

## Out of scope (for now)

Distributed/multi-instance state (global rate limits, shared upstream
discovery), service discovery, TLS termination inside the gateway,
authentication/authorization, request transformation, retries, circuit
breaking, and canary client stickiness. Extension points are marked in code.
