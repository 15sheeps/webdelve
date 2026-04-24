package manager

import (
	"context"
	"fmt"
	"github.com/15sheeps/webdelve/internal/sandbox"
	"github.com/go-delve/delve/service/rpc2"
	"github.com/google/uuid"
	"net"
	"time"
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
	hostPort := container.HostPort()
	addr := net.JoinHostPort("127.0.0.1", hostPort)

	// connect rpc client
	client, err := clientConnRetry(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to delve on port %s", hostPort)
	}

	m.logger.Info("delve server started",
		"host_port", hostPort,
		"container_id", container.ID(), 
	)

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
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
