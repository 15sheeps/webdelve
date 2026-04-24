package main

import (
	"github.com/15sheeps/webdelve/internal/builder"
	"github.com/15sheeps/webdelve/internal/config"
	"github.com/15sheeps/webdelve/internal/logger"
	"github.com/15sheeps/webdelve/internal/manager"
	"github.com/15sheeps/webdelve/internal/sandbox"

	limits "github.com/gin-contrib/size"
	sloggin "github.com/gin-contrib/slog"
	"github.com/gin-gonic/gin"

	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := config.MustLoad()
	baseLogger := logger.New(cfg.Log)
	slog.SetDefault(baseLogger) // for packages that don't receive *slog.Logger

	sandboxLogger := baseLogger.With("component", "sandbox")
	managerLogger := baseLogger.With("component", "manager")

	poolInstance, err := sandbox.NewSandboxPool(ctx, cfg.Sandbox, sandboxLogger)
	if err != nil {
		panic(err)
	}

	// graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		baseLogger.Info("shutting down gracefully...")
		poolInstance.Close()
		os.Exit(0)
	}()

	poolInstance.StartWorkers()

	managerInstance := manager.New(poolInstance, managerLogger)
	builderInstance := builder.NewBuilder(cfg.Builder, poolInstance, managerInstance)

	router := gin.New()

	// middleware to use baseLogger in gin
	router.Use(sloggin.SetLogger(
		sloggin.WithLogger(func(c *gin.Context, l *slog.Logger) *slog.Logger {
			return baseLogger
		}),
	))
	router.Use(corsMiddleware())
	router.Use(gin.Recovery())

	router.POST(
		"/build",
		limits.RequestSizeLimiter(cfg.Server.MaxSourceSize),
		builderInstance.Handle,
	)
	router.GET("/health", healthHandler(poolInstance, managerInstance))
	router.GET("/ws/:session_id", managerInstance.HandleSession)

	router.StaticFile("/", "./web/index.html")

	server := &http.Server{
		Addr:         cfg.Server.Address,
		ReadTimeout:  cfg.Server.RWTimeout,
		WriteTimeout: cfg.Server.RWTimeout,
		Handler:      router,
	}

	baseLogger.Info("server starting...")
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		c.Next()
	}
}

func healthHandler(pool *sandbox.SandboxPool, m *manager.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(200, gin.H{
			"pool_size":       pool.Size(),
			"active_sessions": m.Count(),
		})
	}
}
