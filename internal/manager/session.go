package manager

import (
	"context"
	"fmt"
	"time"

	"github.com/15sheeps/webdelve/internal/sandbox"
	"github.com/go-delve/delve/service/api"
	"github.com/go-delve/delve/service/rpc2"
	"github.com/google/uuid"
)

type Session struct {
	ID        string
	Container *sandbox.Container
	Client    *rpc2.RPCClient
}

func (m *Manager) StartSession(
	ctx context.Context,
	container *sandbox.Container,
	exePath string,
) (*Session, error) {
	addr := container.Address()

	// connect rpc client
	client, err := clientConnRetry(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to delve at %s: %w", addr, err)
	}

	m.logger.Info("connected to the delve server",
		"address", addr,
		"container_id", container.ID(),
	)

	// initial breakpoint on main.main
	if _, err := client.CreateBreakpoint(&api.Breakpoint{
		FunctionName: "main.main",
	}); err != nil {
		client.Disconnect(false) // disconnect without killing delve
		return nil, fmt.Errorf("set initial breakpoint: %w", err)
	}

	// continue to main.main
	select {
	case <-client.Continue():
		/*
			case state := <-contCh:
				if state.Exited {
					client.Disconnect(false)
					return nil, fmt.Errorf("program exited before reaching main.main (status %d)", state.ExitStatus)
				}
		*/
	case <-ctx.Done():
		client.Disconnect(false)
		return nil, ctx.Err()
	}

	sess := &Session{
		ID:        uuid.New().String(),
		Container: container,
		Client:    client,
	}

	m.add(sess)
	return sess, nil
}

func clientConnRetry(ctx context.Context, addr string) (*rpc2.RPCClient, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		client := rpc2.NewClient(addr)
		if client != nil {
			if _, err := client.GetState(); err == nil {
				return client, nil
			}
			client.Disconnect(false)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
