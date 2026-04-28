package handler

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/15sheeps/webdelve/internal/manager"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
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

type WebSocketHandler struct {
	manager *manager.Manager
}

func NewWebSocketHandler(mgr *manager.Manager) *WebSocketHandler {
	return &WebSocketHandler{manager: mgr}
}

func (h *WebSocketHandler) Handle(c *gin.Context) {
	sessionID := c.Param("session_id")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing session id"})
		return
	}

	sess := h.manager.Get(sessionID)
	if sess == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	defer h.manager.Remove(sessionID)

	conn, err := websocket.Accept(c.Writer, c.Request, nil)
	if err != nil {
		h.manager.Logger().Error("websocket accept", "session_id", sessionID, "error", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	h.manager.Logger().Info("websocket connected", "session_id", sessionID)

	// stream program stdout
	stdout, err := sess.Container.StreamLogs(ctx)
	if err != nil {
		h.manager.Logger().Error("stream stdout", "error", err)
	} else {
		go writeStdoutToWs(ctx, conn, stdout)
	}

	// command loop
	for {
		var req CommandRequest
		if err := wsjson.Read(ctx, conn, &req); err != nil {
			h.manager.Logger().Error("websocket read", "session_id", sessionID, "error", err)
			break
		}

		h.manager.Logger().Info("debug command", "session_id", sessionID, "command", req.Command)

		result, err := h.manager.ExecuteCommand(ctx, sess.Client, req.Command, req.Args)
		if err != nil {
			wsjson.Write(ctx, conn, CommandResponse{Type: req.Command, Error: err.Error()})
			continue
		}

		wsjson.Write(ctx, conn, CommandResponse{Type: req.Command, Data: result})
	}

	h.manager.Logger().Info("session closed", "session_id", sessionID)
}

func writeStdoutToWs(ctx context.Context, conn *websocket.Conn, reader io.ReadCloser) {
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		wsjson.Write(ctx, conn, CommandResponse{Data: scanner.Text(), Type: "stdout"})
	}
}
