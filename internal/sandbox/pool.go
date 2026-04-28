package sandbox

import (
	"context"
	"errors"
	"fmt"
	"github.com/15sheeps/webdelve/internal/config"
	"github.com/moby/moby/client"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"
)

// Pool manages a pool of warm containers
type Pool struct {
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

	hostIP string
}

// TODO: make user-defined network for service and sandbox containers
func findHostIP(ctx context.Context, cli *client.Client) (string, error) {
	if _, err := os.Stat("/.dockerenv"); err != nil {
		// must be running natevily on host
		return "127.0.0.1", nil
	}
	// must be running inside docker
	inspectRes, err := cli.NetworkInspect(ctx, "bridge", client.NetworkInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("network inspect error: %w", err)
	}

	// man-made horrors beyond my comprehension
	ipam := inspectRes.Network.IPAM.Config
	if len(ipam) == 0 {
		return "", errors.New("no IPAM config for default bridge network")
	}
	gatewayIP := ipam[0].Gateway.String()
	if gatewayIP == "" {
		return "", errors.New("bridge gateway IP is empty")
	}

	return gatewayIP, nil
}

func NewPool(ctx context.Context, cfg config.SandboxConfig, logger *slog.Logger) (*Pool, error) {
	if cfg.PoolLimit <= 0 {
		return nil, errors.New("pool limit must be positive")
	}

	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize docker client: %w", err)
	}

	hostIP, err := findHostIP(ctx, dockerClient)
	if err != nil {
		return nil, err
	}

	return &Pool{
		client: dockerClient,
		hostIP: hostIP,
		cfg:    cfg,
		logger: logger,
		pool:   make(chan *Container, cfg.PoolLimit),
		sem:    make(chan struct{}, cfg.PoolLimit),
		stop:   make(chan struct{}),
	}, nil
}

// StartWorkers spawn workers that keep the pool filled
func (p *Pool) StartWorkers() {
	p.startOnce.Do(func() {
		limit := p.cfg.PoolLimit
		p.wg.Add(limit)
		for range limit {
			go p.worker()
		}
	})
}

// backoff waits for the configured backoff duration or until pool is closed.
func (p *Pool) backoff() {
	select {
	case <-time.After(time.Second):
	case <-p.stop:
	}
}

// worker creates containers and place them into the pool
func (p *Pool) worker() {
	defer p.wg.Done()
	for { // trying to acquire semaphore slot if stop signal isn't fired
		select {
		case <-p.stop:
			return
		case p.sem <- struct{}{}:
		}

		startCtx, cancel := context.WithTimeout(context.Background(), p.cfg.StartTimeout)
		cont, err := p.startContainer(startCtx)
		cancel()

		if err != nil {
			<-p.sem // release slot
			p.logger.Error("failed to start container", "error", err)
			p.backoff()
			continue
		}

		select { // add container to the pool or clean if pool is closing
		case p.pool <- cont:
		case <-p.stop:
			p.cleanupContainer(cont)
			return
		}
	}
}

// Get retrieves healthy container from the pool
func (p *Pool) Get(ctx context.Context) (*Container, error) {
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

func (p *Pool) Destroy(cont *Container) {
	if cont == nil {
		return
	}
	p.cleanupContainer(cont)
}

// cleanupContainer handles cleanup and release semaphore slot
func (p *Pool) cleanupContainer(cont *Container) {
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
func (p *Pool) Close() {
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

func (p *Pool) IdleCount() int {
	return len(p.pool)
}

// startContainer creates and starts new docker container
func (p *Pool) startContainer(ctx context.Context) (cont *Container, err error) {
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
		return nil, fmt.Errorf("no port binding found for delve (container %s)", delvePort)
	}

	address := net.JoinHostPort(p.hostIP, portBinding[0].HostPort)

	cont = &Container{
		id:      id,
		cli:     p.client,
		address: address,
	}

	return
}
