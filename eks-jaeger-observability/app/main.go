package main

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type application struct {
	logger  *slog.Logger
	tracer  oteltrace.Tracer
	ready   atomic.Bool
	service string
}

func main() {
	rand.Seed(time.Now().UnixNano())

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	serviceName := envOrDefault("OTEL_SERVICE_NAME", "otel-sample-app")
	appEnv := envOrDefault("APP_ENV", "production")
	port := envOrDefault("PORT", "8080")

	tp, err := initTracerProvider(context.Background(), serviceName, appEnv, logger)
	if err != nil {
		logger.Error("failed to initialize OpenTelemetry",
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if shutdownErr := tp.Shutdown(ctx); shutdownErr != nil {
			logger.Error("failed to shutdown OpenTelemetry tracer provider",
				slog.String("error", shutdownErr.Error()),
			)
		}
	}()

	app := &application{
		logger:  logger,
		tracer:  otel.Tracer(serviceName),
		service: serviceName,
	}
	app.ready.Store(true)

	gin.SetMode(envOrDefault("GIN_MODE", gin.ReleaseMode))
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(otelgin.Middleware(serviceName))
	router.Use(app.requestLogger())

	router.GET("/healthz", app.healthz)
	router.GET("/readyz", app.readyz)
	router.GET("/", app.root)
	router.GET("/work", app.work)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		app.logger.Info("sample app started",
			slog.String("service", serviceName),
			slog.String("port", port),
			slog.String("otlp_endpoint", envOrDefault("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "default")),
			slog.String("environment", appEnv),
		)

		if serveErr := server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			app.logger.Error("http server exited unexpectedly",
				slog.String("error", serveErr.Error()),
			)
			os.Exit(1)
		}
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGTERM, syscall.SIGINT)
	sig := <-signalCh

	app.ready.Store(false)
	app.logger.Info("shutdown signal received",
		slog.String("signal", sig.String()),
	)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		app.logger.Error("graceful shutdown failed",
			slog.String("error", err.Error()),
		)
		os.Exit(1)
	}

	app.logger.Info("http server stopped cleanly",
		slog.String("signal", sig.String()),
	)
}

func initTracerProvider(ctx context.Context, serviceName string, appEnv string, logger *slog.Logger) (*sdktrace.TracerProvider, error) {
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}

	sampleRatio := traceSampleRatio(logger)
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithHost(),
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("service.namespace", "observability"),
			attribute.String("deployment.environment", appEnv),
			attribute.String("cloud.provider", "aws"),
			attribute.String("cloud.platform", "aws_eks"),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio))),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	logger.Info("OpenTelemetry initialized",
		slog.Float64("sampling_ratio", sampleRatio),
	)

	return tp, nil
}

func traceSampleRatio(logger *slog.Logger) float64 {
	raw := strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER_ARG"))
	if raw == "" {
		return 0.2
	}

	ratio, err := strconv.ParseFloat(raw, 64)
	if err != nil || ratio < 0 || ratio > 1 {
		logger.Warn("invalid OTEL_TRACES_SAMPLER_ARG, using default",
			slog.String("value", raw),
		)
		return 0.2
	}

	return ratio
}

func (a *application) requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		latency := time.Since(start)
		traceID := currentTraceID(c.Request.Context())
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		fields := []any{
			slog.String("service", a.service),
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", c.Writer.Status()),
			slog.Int64("latency_ms", latency.Milliseconds()),
			slog.String("client_ip", c.ClientIP()),
			slog.String("trace_id", traceID),
		}

		if len(c.Errors) > 0 || c.Writer.Status() >= http.StatusInternalServerError {
			a.logger.Error("request failed", fields...)
			return
		}

		a.logger.Info("request handled", fields...)
	}
}

func (a *application) healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (a *application) readyz(c *gin.Context) {
	if !a.ready.Load() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not-ready"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

func (a *application) root(c *gin.Context) {
	ctx, span := a.tracer.Start(c.Request.Context(), "business.root")
	defer span.End()

	syntheticDelayMs := 25 + rand.Intn(175)
	time.Sleep(time.Duration(syntheticDelayMs) * time.Millisecond)

	span.SetAttributes(
		attribute.Int("app.synthetic_delay_ms", syntheticDelayMs),
		attribute.String("deployment.environment", envOrDefault("APP_ENV", "production")),
	)

	traceID := currentTraceID(ctx)
	a.logger.InfoContext(ctx, "handled root business flow",
		slog.String("path", "/"),
		slog.String("trace_id", traceID),
		slog.Int("synthetic_delay_ms", syntheticDelayMs),
	)

	c.JSON(http.StatusOK, gin.H{
		"message":          "Distributed tracing is active",
		"service":          a.service,
		"traceId":          traceID,
		"syntheticDelayMs": syntheticDelayMs,
	})
}

func (a *application) work(c *gin.Context) {
	ctx, span := a.tracer.Start(c.Request.Context(), "simulate.checkout.flow")
	defer span.End()

	if err := a.inventoryLookup(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		a.logger.ErrorContext(ctx, "inventory lookup failed",
			slog.String("error", err.Error()),
			slog.String("trace_id", currentTraceID(ctx)),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "inventory_lookup_failed"})
		return
	}

	if err := a.paymentAuthorization(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		a.logger.ErrorContext(ctx, "payment authorization failed",
			slog.String("error", err.Error()),
			slog.String("trace_id", currentTraceID(ctx)),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "payment_authorization_failed"})
		return
	}

	traceID := currentTraceID(ctx)
	a.logger.InfoContext(ctx, "checkout workflow completed",
		slog.String("path", "/work"),
		slog.String("trace_id", traceID),
	)

	c.JSON(http.StatusOK, gin.H{
		"status":  "completed",
		"traceId": traceID,
	})
}

func (a *application) inventoryLookup(ctx context.Context) error {
	ctx, span := a.tracer.Start(ctx, "inventory.lookup")
	defer span.End()

	time.Sleep(40 * time.Millisecond)
	span.SetAttributes(attribute.Bool("inventory.cache_hit", false))

	a.logger.InfoContext(ctx, "inventory lookup finished",
		slog.Bool("cache_hit", false),
		slog.String("trace_id", currentTraceID(ctx)),
	)

	return nil
}

func (a *application) paymentAuthorization(ctx context.Context) error {
	ctx, span := a.tracer.Start(ctx, "payment.authorization")
	defer span.End()

	time.Sleep(90 * time.Millisecond)
	span.SetAttributes(attribute.String("payment.provider", "demo"))

	a.logger.InfoContext(ctx, "payment authorization finished",
		slog.String("provider", "demo"),
		slog.String("trace_id", currentTraceID(ctx)),
	)

	return nil
}

func currentTraceID(ctx context.Context) string {
	span := oteltrace.SpanFromContext(ctx)
	if !span.SpanContext().HasTraceID() {
		return ""
	}

	return span.SpanContext().TraceID().String()
}

func envOrDefault(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}
