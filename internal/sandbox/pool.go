package sandbox

import (
	"context"
	"errors"
	"fmt"
	"github.com/15sheeps/webdelve/internal/config"
	"github.com/moby/moby/client"
	"log/slog"
	"sync"
	"time"
)

// Sandbox manages a pool of warm containers
type SandboxPool struct {
	client *client.Client // docker client
	cfg    config.SandboxConfig
	logger *slog.Logger

	pool chan *Container // pool of idle containers
	sem  chan struct{}   // limits amount of containers (running + idle)

	mu        sync.Mutex
	wg        sync.WaitGroup
	startOnce sync.Once
	closed    bool
	stop      chan struct{}
}

func NewSandboxPool(ctx context.Context, cfg config.SandboxConfig, logger *slog.Logger) (*SandboxPool, error) {
	if cfg.PoolLimit <= 0 {
		return nil, errors.New("pool limit must be positive")
	}

	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize docker client: %w", err)
	}

	return &SandboxPool{
		client: dockerClient,
		cfg:    cfg,
		logger: logger,
		pool:   make(chan *Container, cfg.PoolLimit),
		sem:    make(chan struct{}, cfg.PoolLimit),
		stop:   make(chan struct{}),
	}, nil
}

// StartWorkers spawn workers that keep the pool filled
func (p *SandboxPool) StartWorkers() {
	p.startOnce.Do(func() {
		limit := p.cfg.PoolLimit
		p.wg.Add(limit)
		for range limit {
			go p.worker()
		}
	})
}

// backoff waits for the configured backoff duration or until shutdown.
func (p *SandboxPool) backoff() {
	select {
	case <-time.After(time.Second):
	case <-p.stop:
	}
}

// worker creates containers and place them into pool
func (p *SandboxPool) worker() {
	defer p.wg.Done()
	for {
		select {
		case <-p.stop:
			return
		case p.sem <- struct{}{}: // acquire a slot before starting container
			startCtx, cancel := context.WithTimeout(context.Background(), p.cfg.StartTimeout)
			cont, err := p.startContainer(startCtx)
			cancel()

			if err != nil {
				p.logger.Error("failed to start container", "error", err)
				<-p.sem
				p.backoff()
				continue
			}

			select { // hand off to pool or cleanup if pool closes
			case p.pool <- cont:
			case <-p.stop:
				p.cleanupContainer(cont)
				return
			}
		}
	}
}

// Get retrieves healthy container from the pool
func (p *SandboxPool) Get(ctx context.Context) (*Container, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("pool is closed")
	}
	p.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-p.stop:
			return nil, errors.New("pool is shutting down")
		case cont := <-p.pool:
			// verify container is healthy
			healthCtx, cancel := context.WithTimeout(ctx, p.cfg.HealthTimeout)
			err := cont.HealthCheck(healthCtx)
			cancel()
			if err == nil {
				return cont, nil
			}

			p.logger.Info("cleaning up unhealthy container",
				"container_id", cont.ID(),
				"error", err,
			)
			p.cleanupContainer(cont)
		}
	}
}

func (p *SandboxPool) Put(cont *Container) {
	if cont == nil {
		return
	}
	go p.cleanupContainer(cont)
}

// cleanupContainer handles cleanup and release semaphore slot
func (p *SandboxPool) cleanupContainer(cont *Container) {
	defer func() { <-p.sem }()

	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.RemoveTimeout)
	defer cancel()

	if err := cont.remove(ctx); err != nil {
		p.logger.Error("failed to remove container",
			"container_id", cont.id,
			"error", err,
		)
	}
}

// Close gracefully shuts down the pool
func (p *SandboxPool) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	close(p.stop)
	p.mu.Unlock()

	p.wg.Wait() // wait for all workers to exit

	// drain the pool and clean remaining containers
	for {
		select {
		case cont := <-p.pool:
			p.cleanupContainer(cont)
		default:
			return
		}
	}
}

func (p *SandboxPool) Size() int {
	return len(p.pool)
}

// startContainer creates and starts new docker container
func (p *SandboxPool) startContainer(ctx context.Context) (cont *Container, err error) {
	createOpts := CreateOptions(p.cfg)
	createResp, err := p.client.ContainerCreate(ctx, createOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w", err)
	}

	for _, warning := range createResp.Warnings {
		p.logger.Warn(warning)
	}
	id := createResp.ID

	defer func() {
		if err != nil {
			p.client.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
		}
	}()

	_, err = p.client.ContainerStart(
		ctx, id,
		client.ContainerStartOptions{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start container %s: %w", id, err)
	}

	// get assigned port
	info, err := p.client.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("container inspect failed: %w", err)
	}

	portBinding := info.Container.NetworkSettings.Ports[delvePort]
	if len(portBinding) == 0 {
		return nil, fmt.Errorf("no port binding found for delve listening port %s", delvePort)
	}

	cont = &Container{
		id:       id,
		cli:      p.client,
		hostPort: portBinding[0].HostPort,
	}

	return
}
