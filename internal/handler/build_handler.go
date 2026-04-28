package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"time"

	"github.com/15sheeps/webdelve/internal/builder"
	"github.com/15sheeps/webdelve/internal/config"
	"github.com/15sheeps/webdelve/internal/manager"
	"github.com/15sheeps/webdelve/internal/metrics"
	"github.com/15sheeps/webdelve/internal/sandbox"
)

type BuildHandler struct {
	builder *builder.Builder
	pool    *sandbox.Pool
	manager *manager.Manager
}

func NewBuildHandler(cfg config.BuilderConfig, pool *sandbox.Pool, mgr *manager.Manager) *BuildHandler {
	bld := builder.NewBuilder(cfg)
	return &BuildHandler{builder: bld, pool: pool, manager: mgr}
}

type buildResponse struct {
	Formatted string `json:"formatted,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

type buildRequest struct {
	Code string `json:"code" binding:required`
}

// Handle handles POST request to compile provided source code
func (h *BuildHandler) Handle(c *gin.Context) {
	start := time.Now()
	var req buildRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, buildResponse{
			Error: "invalid request: " + err.Error(),
		})
		return
	}

	ctx := c.Request.Context()

	// build the binary from source code
	result, err := h.builder.Build(ctx, []byte(req.Code))
	if err != nil {
		metrics.BuildErrorsTotal.Inc()
		c.JSON(http.StatusBadRequest, buildResponse{Error: err.Error()})
		return
	}
	metrics.BuildDuration.Observe(time.Since(start).Seconds())
	defer builder.Cleanup(result)

	// get a warm container from the pool
	container, err := h.pool.Get(ctx)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, buildResponse{Error: "no available sandbox"})
		return
	}

	// upload binary to the container
	if err := builder.Upload(ctx, container, result.ExePath); err != nil {
		h.pool.Destroy(container)
		c.JSON(http.StatusInternalServerError, buildResponse{Error: "failed to upload binary: " + err.Error()})
		return
	}

	// start delve server in the container
	sess, err := h.manager.StartSession(ctx, container, "/program")
	if err != nil {
		h.pool.Destroy(container)
		c.JSON(http.StatusInternalServerError, buildResponse{Error: "failed to start debug session: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, buildResponse{
		Formatted: result.Formatted,
		SessionID: sess.ID,
	})
}
