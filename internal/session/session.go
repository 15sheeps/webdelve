package session

import (
	"context"
	"fmt"
	"log"
	"time"
	"sync"
	"net"
	"github.com/go-delve/delve/service/rpc2"
	"github.com/google/uuid"
	"github.com/15sheeps/webdelve/internal/sandbox"
)

type Session struct {
	ID string
	Container *sandbox.Container
	Client	  *rpc2.RPCClient
	CreatedAt time.Time
	mu        sync.Mutex
}

func Start(
	ctx context.Context, 
	container *sandbox.Container, 
	exePath string,
) (*Session, error) {
	// start delve process
	cmd := []string{
		"dlv", "exec", exePath, "--listen=:2345", "--headless",
		"--api-version=2", "--accept-multiclient", 
	}

	if _, err := container.Exec(ctx, cmd); err != nil {
		return nil, err
	}

	// wait till debugger start listening on :2345
	hostPort := container.HostPort()
	addr := net.JoinHostPort("127.0.0.1", hostPort)

	// connect rpc client
	client, err := clientConnRetry(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to delve on port %s", hostPort)
	}

	log.Printf("delve started on port %s in container %s", hostPort, container.ID())

	return &Session{
		ID: uuid.New().String(),
		Container: container,
		Client: client,
		CreatedAt: time.Now(),	
	}, nil
}

func clientConnRetry(ctx context.Context, addr string) (*rpc2.RPCClient, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
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
