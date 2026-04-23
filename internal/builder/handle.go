package builder

import (
	"log"
	"context"
	"net/http"
	"github.com/gin-gonic/gin"
	"github.com/15sheeps/webdelve/internal/session"
)

type BuildRequest struct {
	Code string `json:"code" binding:"required"`
}

type BuildResponse struct {
	Formatted string `json:"formatted",omitempty`
	SessionID string `json:"session_id",omitempty`
	Error	  string `json:"error",omitempty`
	exePath   string
}

// Handles POST request to compile provided source code
func (b *Builder) Handle(c *gin.Context) {
	var req BuildRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, BuildResponse{
			Error: "invalid request: " + err.Error(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), b.cfg.BuildTimeout)
	defer cancel()

	buildRes, err := b.Build(ctx, []byte(req.Code))
	if err != nil {
		c.JSON(http.StatusBadRequest, BuildResponse{Error: err.Error()})
		return
	}
	// defer Cleanup(buildRes)
	// get container from the pool
	container, err := b.pool.Get(ctx)
	if err != nil {
		c.JSON(http.StatusBadRequest, BuildResponse{
			Error: "no available sandbox",
		})
		return
	}
	// upload binary to the container
	if err := Upload(ctx, container, buildRes.exePath); err != nil {
		b.pool.Put(container)
		c.JSON(http.StatusInternalServerError, BuildResponse{
			Error: "failed to upload binary: " + err.Error(),
		})
		return
	}
	// start delve server in the container
	sess, err := session.Start(ctx, container, "/workdir/program")
	if err != nil {
		b.pool.Put(container)
		c.JSON(http.StatusInternalServerError, BuildResponse{
			Error: "failed to start debug session: " + err.Error(),
		})
		return
	}

	b.manager.Add(sess)

	log.Printf("Session %s created, container %s, port: %s\n",
		sess.ID, container.ID(), container.HostPort(),
	)

	buildRes.SessionID = sess.ID
	c.JSON(http.StatusOK, buildRes)
}
