package main

import (
	"context"
	"net/http"
	"log"
	"os"
	"os/signal"
	"syscall"
	"github.com/gin-gonic/gin"
	limits "github.com/gin-contrib/size"
	"github.com/15sheeps/webdelve/internal/sandbox"
	"github.com/15sheeps/webdelve/internal/builder"
	"github.com/15sheeps/webdelve/internal/session"
)

const maxSourceSize = 1 << 20

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poolCfg := sandbox.DefaultConfig()
	pool, err := sandbox.NewSandboxPool(ctx, poolCfg)
	if err != nil {
		log.Fatalf("failed to create sandbox: %v\n", err)
	}

	// graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("shutting down gracefully...")
		pool.Close()
		os.Exit(0)
	}()
	
	pool.StartWorkers()

	manager := session.NewManager(pool)
	b := builder.NewBuilder(builder.DefaultConfig(), pool, manager)

	r := gin.New()
	r.Use(gin.Logger())
	r.Use(corsMiddleware())

	r.POST("/build", limits.RequestSizeLimiter(maxSourceSize), b.Handle)
	r.GET("/health", healthHandler(pool, manager))
	
	r.GET("/ws/:session_id", manager.HandleSession)

    r.StaticFile("/", "./web/index.html")

	log.Println("server starting on :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalf("server faled: %v\n", err)
	}
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		c.Next()
	}
}

func healthHandler(pool *sandbox.SandboxPool, manager *session.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(200, gin.H{
			"pool_size": pool.Size(),
			"active_sessions": manager.Count(),
		})
	}
}
