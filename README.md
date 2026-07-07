# Payment Service

This repository contains the `payment` service used by the enhanced Sock Shop benchmark for the paper:

> EviRCA: An Evidence-Aware Skill-Based LLM Agent and a Telemetry-Rich Multi-Modal Benchmark for Microservice Root Cause Analysis

The service is derived from the original Sock Shop payment microservice and has been modernized for reproducible microservice RCA experiments. In the benchmark, `payment` is one of the Go services, together with `catalogue` and `user`, migrated to Go 1.22 and Go Modules with improved runtime configuration, Prometheus metrics, trace-aware logging, and Zipkin/OpenTracing instrumentation.

## Role in Enhanced Sock Shop

`payment` authorizes checkout payments for Sock Shop. It exposes a small HTTP API, but it is instrumented so that benchmark runs can collect synchronized metrics, logs, and traces for service-level, pod-level, and entity-fault RCA.

Key benchmark-oriented changes include:

- Go 1.22 module-based build instead of the legacy GOPATH/gvt workflow.
- Prometheus HTTP metrics for request duration, in-flight requests, request body size, and response body size.
- Zipkin/OpenTracing spans for incoming `/paymentAuth` requests and the internal authorization operation.
- Kubernetes metadata tags on spans from `CONTAINER_NAME`, `POD_NAME`, `POD_NAMESPACE`, and `NODE_NAME`.
- Trace-aware structured logs with trace ID, span ID, operation, target, error type, and latency fields.
- Reduced observability noise by excluding `/health` from tracing and service-level request logging.
- Clearer validation and transport error classification for RCA-friendly log and span evidence.

## API

### `GET /health`

Returns the service health state.

```bash
curl http://localhost:8082/health
```

Example response:

```json
{
  "health": [
    {
      "service": "payment",
      "status": "OK",
      "time": "2026-07-07 12:00:00 +0000 UTC"
    }
  ]
}
```

### `POST /paymentAuth`

Authorizes a payment amount.

```bash
curl -H "Content-Type: application/json" \
  -X POST \
  -d '{"amount":40}' \
  http://localhost:8082/paymentAuth
```

Example response:

```json
{
  "authorised": true,
  "message": "Payment authorised"
}
```

Payments are declined when the amount exceeds the configured decline threshold. Zero, negative, missing, or invalid amounts are treated as validation errors.

### `GET /metrics`

Exposes Prometheus metrics.

```bash
curl http://localhost:8082/metrics
```

Representative metrics include:

- `http_request_duration_seconds`
- `http_inflight_requests`
- `http_request_body_size_bytes`
- `http_response_body_size_bytes`

## Configuration

The service binary is `cmd/paymentsvc`.

| Option | Default | Description |
| --- | --- | --- |
| `-port` | `80` | HTTP listen port. |
| `-decline` | `100000000` | Decline payments over this amount. |
| `-zipkin` | resolved from environment | Zipkin collector base URL. Empty disables tracing. |

Zipkin address resolution uses the first non-empty value from:

1. `ZIPKIN`
2. `ZIPKIN_BASE_URL`
3. `http://${ZIPKIN_HOST}:${ZIPKIN_PORT}`

If none are set, the default is:

```text
http://jaeger-collector.observability.svc.cluster.local:9411
```

Span metadata can be enriched with Kubernetes runtime context:

| Environment variable | Span tag |
| --- | --- |
| `CONTAINER_NAME` | `container` |
| `POD_NAME` | `pod` |
| `POD_NAMESPACE` | `namespace` |
| `NODE_NAME` | `node` |

## Build

```bash
go build -o payment ./cmd/paymentsvc
```

## Run Locally

Tracing can be disabled by passing an empty Zipkin address.

```bash
./payment -port 8082 -zipkin ""
```

Then call:

```bash
curl http://localhost:8082/health
curl -H "Content-Type: application/json" -d '{"amount":40}' http://localhost:8082/paymentAuth
curl http://localhost:8082/metrics
```

## Run with Docker Compose

```bash
docker-compose up --build
```

The service is exposed at:

```text
http://localhost:8082
```

To run with Zipkin support:

```bash
docker-compose -f docker-compose.yml -f docker-compose-zipkin.yml up --build
```

## Test

Run the Go test suite:

```bash
go test ./...
```

The tests cover authorization behavior, HTTP transport behavior, trace propagation, trace tags, and main package runtime helpers.

The legacy container test flow is still available when Docker is configured:

```bash
COMMIT=test make test
```

## Repository Contents

- `cmd/paymentsvc/`: service entrypoint and runtime configuration.
- `service.go`: payment authorization domain logic.
- `endpoints.go`: Go kit endpoints, trace spans, trace metadata, and endpoint logging.
- `transport.go`: HTTP routing, JSON encoding/decoding, error handling, and `/metrics`.
- `wiring.go`: service wiring and Prometheus middleware.
- `api-spec/payment.json`: Swagger 2.0 API specification.
- `docker-compose.yml`: local service deployment on port `8082`.
- `docker-compose-zipkin.yml`: optional Zipkin deployment overlay.
- `test/`: legacy Dredd/container integration test harness.
