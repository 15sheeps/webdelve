package manager

import (
	"context"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
	"github.com/go-delve/delve/service/api"
	"net/http"
	"time"
)

// CommandRequest represents a debugger request from client
type CommandRequest struct {
	Command string         `json:"command"`
	Args    map[string]any `json:"args,omitempty"`
}

// CommandRequest represents a debugger response to client
type CommandResponse struct {
	Type  string `json:"type"`
	Data  any    `json:"data"`
	Error string `json:"error,omitempty"`
}

func (m *Manager) HandleSession(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing session id"})
		return
	}

	sess := m.Get(sessionID)
	if sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	defer m.Remove(sessionID)

	// initial breakpoint
	_, err := sess.Client.CreateBreakpoint(&api.Breakpoint{
		FunctionName: "main.main",
	})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "failed to set initial breakpoint"})
		return
	}
	// step till that initial bp
	select {
	case <-sess.Client.Continue():
	case <-time.After(5 * time.Second):
		c.JSON(http.StatusNotFound, gin.H{"error": "failed to step to initial breakpoint"})
		return
	}

	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		m.logger.Error("websocket accept",
			"session_id", sessionID,
			"error", err,
		)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Minute)
	defer cancel()

	m.logger.Info("websocket client connected", "session_id", sessionID)

	for {
		var req CommandRequest
		if err := wsjson.Read(ctx, conn, &req); err != nil {
			m.logger.Error("websocket read",
				"session_id", sessionID,
				"error", err,
			)
			break
		}

		m.logger.Info("received debugging command", 
			"session_id", sessionID,
			"command", req.Command,
		)

		result, err := m.executeCommand(ctx, sess.Client, req.Command, req.Args)
		if err != nil {
			wsjson.Write(ctx, conn, CommandResponse{
				Type:  req.Command,
				Error: err.Error(),
			})
			continue
		}

		wsjson.Write(ctx, conn, CommandResponse{
			Type: req.Command,
			Data: result,
		})
	}

	m.logger.Info("session closed", "session_id", sessionID)
}
