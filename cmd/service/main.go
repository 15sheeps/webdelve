package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/15sheeps/webdelve/internal/config"
	"github.com/15sheeps/webdelve/internal/handler"
	"github.com/15sheeps/webdelve/internal/logger"
	"github.com/15sheeps/webdelve/internal/manager"
	"github.com/15sheeps/webdelve/internal/metrics"
	"github.com/15sheeps/webdelve/internal/sandbox"
	"github.com/15sheeps/webdelve/web"

	limits "github.com/gin-contrib/size"
	sloggin "github.com/gin-contrib/slog"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.MustLoad()
	baseLogger := logger.New(cfg.Log)
	slog.SetDefault(baseLogger)

	sandboxLogger := baseLogger.With("component", "sandbox")
	managerLogger := baseLogger.With("component", "manager")

	staticFS, err := fs.Sub(web.StaticFiles, ".")
	if err != nil {
		baseLogger.Error("failed to initialize static FS", "error", err)
		os.Exit(1)
	}

	sandboxPool, err := sandbox.NewPool(ctx, cfg.Sandbox, sandboxLogger)
	if err != nil {
		baseLogger.Error("failed to create new sandbox pool", "error", err)
		os.Exit(1)
	}
	sandboxPool.StartWorkers()

	prometheus.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "sandbox_idle",
			Help: "Number of idle sandbox containers.",
		},
		func() float64 { return float64(sandboxPool.IdleCount()) },
	))

	managerInstance := manager.New(sandboxPool, managerLogger)

	// handlers
	buildHandler := handler.NewBuildHandler(cfg.Builder, sandboxPool, managerInstance)
	wsHandler := handler.NewWebSocketHandler(managerInstance)

	router := gin.New()
	// middleware to use baseLogger for gin requests
	router.Use(sloggin.SetLogger(
		sloggin.WithLogger(func(c *gin.Context, l *slog.Logger) *slog.Logger {
			return baseLogger
		}),
	))
	router.Use(corsMiddleware())
	router.Use(gin.Recovery())
	router.Use(prometheusMiddleware())

	router.POST(
		"/build",
		limits.RequestSizeLimiter(cfg.Server.MaxSourceSize),
		buildHandler.Handle,
	)

	router.GET("/ws/:session_id", wsHandler.Handle)
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// static frontend
	router.StaticFS("/app", http.FS(staticFS))
	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/app/")
	})

	server := &http.Server{
		Addr:         cfg.Server.Address,
		ReadTimeout:  cfg.Server.RWTimeout,
		WriteTimeout: cfg.Server.RWTimeout,
		Handler:      router,
	}

	// channel to ensure shutdown completes before main exits
	shutdown := make(chan struct{})

	// graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		defer func() {
			shutdown <- struct{}{}
		}()

		<-sigChan
		baseLogger.Info("shutting down...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			baseLogger.Error("server shutdown error", "error", err)
		}

		baseLogger.Info("closing sandbox pool...")
		sandboxPool.Close()
		baseLogger.Info("sandbox pool closed, all containers removed")
	}()

	baseLogger.Info("server starting...")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		baseLogger.Error("server failed", "error", err)
		os.Exit(1)
	}

	// wait for shutdown to complete before exiting
	baseLogger.Info("waiting for cleanup to complete...")
	<-shutdown
	baseLogger.Info("shutdown complete")
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		c.Next()
	}
}

func prometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/metrics" {
			c.Next()
			return
		}

		start := time.Now()
		path := c.FullPath()
		if path == "" {
			path = "other"
		}
		method := c.Request.Method

		status := fmt.Sprintf("%d", c.Writer.Status())

		metrics.HTTPRequestsTotal.WithLabelValues(method, path, status).
			Inc()
		metrics.HTTPRequestDuration.WithLabelValues(method, path).
			Observe(time.Since(start).Seconds())
	}
}
