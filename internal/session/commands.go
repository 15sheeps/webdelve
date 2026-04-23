package session

import (
	"github.com/go-delve/delve/service/rpc2"
	"github.com/go-delve/delve/service/api"
	"context"
	"fmt"
)

var defaultLoadCfg = api.LoadConfig{
	FollowPointers:     true,
	MaxVariableRecurse: 2,
	MaxStringLen:       64,
	MaxArrayValues:     10,
	MaxStructFields:    10,
}

func (m *Manager) executeCommand(
	ctx context.Context, 
	client *rpc2.RPCClient, 
	cmd string,
	args map[string]any,
) (any, error) {
	switch cmd {
	case "next":
		return client.Next()
	case "step":
		return client.Step()
	case "stepout":
		return client.StepOut()
	case "breakpoints":
		return client.ListBreakpoints(false)
	case "restart":
		// Restart(rebuild bool) ([]api.DiscardedBreakpoint, error)
		// binary wasn't build by delve so rebuild isn't possible
		_, err := client.Restart(false)
		if err != nil {
			return nil, err
		}
		// update state after restart
		fallthrough
	case "state":
		return client.GetState()
	case "continue":
		select {
		case state := <-client.Continue():
			return state, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}

	case "stack":
		goroutineID := int64(-1) // current goroutine
		if gid, ok := args["goroutineId"].(float64); ok {
			goroutineID = int64(gid)
		}
		depth := 50
		if d, ok := args["depth"].(float64); ok {
			depth = int(d)
		}
		skip := 0
		if s, ok := args["skip"].(float64); ok {
			skip = int(s)
		}
		// Stacktrace(goroutineId int64, depth, skip int, opts api.StacktraceOptions, cfg *api.LoadConfig)
		return client.Stacktrace(goroutineID, depth, skip, 0, &defaultLoadCfg)

	case "locals":
		// Сначала получаем состояние для scope
		state, err := client.GetState()
		if err != nil {
			return nil, err
		}
		if state.CurrentThread == nil {
			return nil, fmt.Errorf("no current thread")
		}
		
		scope := api.EvalScope{
			GoroutineID: state.CurrentThread.GoroutineID,
			Frame:       0,
		}
		
		return client.ListLocalVariables(scope, defaultLoadCfg)

	case "goroutines":
		goroutines, _, err := client.ListGoroutines(0, 50)
		return goroutines, err

	case "break":
		line := 0
		if l, ok := args["line"].(float64); ok {
			line = int(l)
		}
		file := ""
		if f, ok := args["file"].(string); ok {
			file = f
		}
		return client.CreateBreakpoint(&api.Breakpoint{
			File: file,
			Line: line,
		})

	case "clear":
		id := 0
		if i, ok := args["id"].(float64); ok {
			id = int(i)
		}
		return client.ClearBreakpoint(id)

	default:
		return nil, fmt.Errorf("unknown command: %s", cmd)
	}
}
