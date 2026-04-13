# Observability Helpers

This folder contains shared Go code used by both microservices.

Current services using this folder:

- `checkout-service`
- `inventory-service`

## Why This Folder Exists

Both services need the same observability logic:

- OpenTelemetry tracer setup
- JSON structured logging
- request logging middleware
- trace ID helper
- environment variable helper

Instead of writing the same code in both services, that common logic is kept here once and reused.

So this folder helps:

- reduce duplicate code
- keep both services consistent
- make tracing setup easier to maintain

## What `telemetry.go` Does

File:

- `telemetry.go`

This file contains the shared observability functions for both microservices.

Main uses:

1. `NewJSONLogger()`
   Creates a structured JSON logger using Go `slog`.

2. `InitTracerProvider(...)`
   Initializes OpenTelemetry and configures the OTLP trace exporter.

3. `RequestLogger(...)`
   Adds request logs for every HTTP request with:
   - method
   - path
   - status
   - latency
   - client IP
   - trace ID

4. `CurrentTraceID(...)`
   Reads the current trace ID from the request context.

5. `EnvOrDefault(...)`
   Reads an environment variable and falls back to a default value.

## Why `internal` Is Used

The parent folder is named `internal` because in Go this means:

- the code is private to this project
- other outside Go modules should not import it directly

That is good here because this code is only for your own microservices inside this app.

## Simple Flow

When `checkout-service` or `inventory-service` starts:

1. service imports `internal/observability`
2. service creates a logger using `NewJSONLogger()`
3. service creates OpenTelemetry tracing using `InitTracerProvider(...)`
4. service uses `RequestLogger(...)` for HTTP logs
5. traces go to OpenTelemetry Collector

## In Short

This folder is the shared observability utility folder.

`telemetry.go` is used to give both microservices:

- the same tracing setup
- the same logging style
- the same helper functions

So instead of writing tracing code two times, both services reuse one common file.
