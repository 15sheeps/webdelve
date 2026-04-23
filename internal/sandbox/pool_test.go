package sandbox

import (
	"context"
	"fmt"
	"io"
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
	"github.com/moby/moby/client"
	"github.com/docker/docker/pkg/stdcopy"
)

func TestNewSandboxPool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PoolLimit = 3

	pool, err := NewSandboxPool(context.Background(), cfg)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer pool.Close()

	if pool == nil {
		t.Fatal("pool is nil")
	}

	if cap(pool.sem) != 3 {
		t.Errorf("expected sem capacity 3, got %d", cap(pool.sem))
	}

	if pool.cfg.PoolLimit != 3 {
		t.Errorf("expected pool limit 3, got %d", pool.cfg.PoolLimit)
	}
}

func TestNewSandboxEmptyPool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PoolLimit = 0

	_, err := NewSandboxPool(context.Background(), cfg)
	if err == nil {
		t.Error("expected error for zero pool limit")
	}
}

func TestStartWorkers(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PoolLimit = 2
	cfg.StartTimeout = 15 * time.Second

	pool, err := NewSandboxPool(context.Background(), cfg)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer pool.Close()

	pool.StartWorkers()

	if !waitForPoolSize(t, pool, 2, 20*time.Second) {
		t.Fatalf("pool not filled, size: %d", pool.Size())
	}

	t.Logf("Pool filled with %d containers", pool.Size())
}

func TestGetPut(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PoolLimit = 1
	cfg.StartTimeout = 15 * time.Second
	cfg.HealthTimeout = 5 * time.Second

	pool, err := NewSandboxPool(context.Background(), cfg)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer pool.Close()

	pool.StartWorkers()

	if !waitForPoolSize(t, pool, 1, 20*time.Second) {
		t.Fatal("pool never filled")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cont1, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get container: %v", err)
	}

	if cont1 == nil {
		t.Fatal("got nil container")
	}

	if cont1.ID() == "" {
		t.Error("container ID is empty")
	}

	if cont1.HostPort() == "" {
		t.Error("host port is empty")
	}

	t.Logf("Got container: id=%s, port=%s", cont1.ID(), cont1.HostPort())

	if err := cont1.HealthCheck(ctx); err != nil {
		t.Errorf("container health check failed: %v", err)
	}

	id1 := cont1.ID()
	pool.Put(cont1)

	if !waitForPoolSize(t, pool, 1, 20*time.Second) {
		t.Fatal("pool not refilled after put")
	}

	cont2, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get second container: %v", err)
	}
	defer pool.Put(cont2)

	if id1 == cont2.ID() {
		t.Error("container was not recreated, same ID")
	}

	t.Logf("Second container: id=%s, port=%s", cont2.ID(), cont2.HostPort())
}

func TestContainerExec(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PoolLimit = 1
	cfg.StartTimeout = 15 * time.Second

	pool, err := NewSandboxPool(context.Background(), cfg)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer pool.Close()

	pool.StartWorkers()

	if !waitForPoolSize(t, pool, 1, 20*time.Second) {
		t.Fatal("pool never filled")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cont, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get container: %v", err)
	}
	defer pool.Put(cont)

	tests := []struct {
		name    string
		cmd     []string
		want    string
		wantErr bool
	}{
		{
			name: "echo command",
			cmd:  []string{"echo", "hello world"},
			want: "hello world",
		},
		{
			name: "shell pipeline",
			cmd:  []string{"sh", "-c", "echo test > /tmp/test.txt && cat /tmp/test.txt"},
			want: "test",
		},
		{
			name: "list root",
			cmd:  []string{"ls", "/"},
			want: "bin",
		},
		{
			name: "check delve exists",
			cmd:  []string{"which", "dlv"},
			want: "/usr/local/bin/dlv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			execCtx, execCancel := context.WithTimeout(ctx, 5*time.Second)
			defer execCancel()

			reader, err := cont.Exec(execCtx, tt.cmd)
			if err != nil {
				if !tt.wantErr {
					t.Fatalf("exec failed: %v", err)
				}
				return
			}

			output, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("failed to read output: %v", err)
			}

			if !strings.Contains(string(output), tt.want) {
				t.Errorf("expected output to contain %q, got %q", tt.want, string(output))
			}
		})
	}
}

func TestContainerCopyTo(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PoolLimit = 1
	cfg.StartTimeout = 15 * time.Second

	pool, err := NewSandboxPool(context.Background(), cfg)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer pool.Close()

	pool.StartWorkers()

	if !waitForPoolSize(t, pool, 1, 20*time.Second) {
		t.Fatal("pool never filled")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cont, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get container: %v", err)
	}
	defer pool.Put(cont)

	var stdout, stderr bytes.Buffer

	// test 1: copying the file
	testContent := []byte("Hello, Sandbox!")
	err = cont.CopyFileTo(ctx, "/sandbox/testfile.txt", testContent)
	if err != nil {
		t.Fatalf("copy file to container failed: %v", err)
	}

	// verify file copy
	reader, err := cont.Exec(ctx, []string{"cat", "/sandbox/testfile.txt"})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
    stdout.Reset()
    stderr.Reset()

	_, err = stdcopy.StdCopy(&stdout, &stderr, reader)
	if err != nil {
	    t.Fatalf("failed to read: %v", err)
	}
	output := stdout.Bytes() 

	if string(output) != string(testContent) {
		t.Errorf("expected %q, got %q", string(testContent), string(output))
	}
}

func TestContainerHealthCheck(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PoolLimit = 1
	cfg.StartTimeout = 15 * time.Second

	pool, err := NewSandboxPool(context.Background(), cfg)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer pool.Close()

	pool.StartWorkers()

	if !waitForPoolSize(t, pool, 1, 20*time.Second) {
		t.Fatal("pool never filled")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cont, err := pool.Get(ctx)
	if err != nil {
		t.Fatalf("failed to get container: %v", err)
	}

	if err := cont.HealthCheck(ctx); err != nil {
		t.Errorf("healthy container failed health check: %v", err)
	}

	pool.Put(cont)

	time.Sleep(2 * time.Second)
	if err := cont.HealthCheck(ctx); err == nil {
		t.Error("removed container should fail health check")
	}
}

func TestPoolClose(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PoolLimit = 2
	cfg.StartTimeout = 15 * time.Second

	pool, err := NewSandboxPool(context.Background(), cfg)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}

	pool.StartWorkers()

	if !waitForPoolSize(t, pool, 2, 20*time.Second) {
		t.Fatal("pool never filled")
	}


	var containerIDs []string
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for i := 0; i < 2; i++ {
		cont, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("failed to get container: %v", err)
		}
		containerIDs = append(containerIDs, cont.ID())
		pool.Put(cont)
	}

	pool.Close()

	time.Sleep(5 * time.Second)

	for _, id := range containerIDs {
		if containerExists(t, pool.client, id) {
			t.Errorf("container %s still exists after pool close", id)
		}
	}

	_, err = pool.Get(context.Background())
	if err == nil {
		t.Error("expected error when getting from closed pool")
	}
}

// TestContextCancellation проверяет обработку отмены контекста при Get
func TestContextCancellation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PoolLimit = 1 // пустой пул

	pool, err := NewSandboxPool(context.Background(), cfg)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer pool.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Отменяем контекст сразу
	cancel()

	_, err = pool.Get(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestMultipleContainers(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PoolLimit = 3
	cfg.StartTimeout = 20 * time.Second

	pool, err := NewSandboxPool(context.Background(), cfg)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer pool.Close()

	pool.StartWorkers()

	if !waitForPoolSize(t, pool, 3, 30*time.Second) {
		t.Fatal("pool never filled")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// get all containers from pool
	var containers []*Container
	for i := 0; i < 3; i++ {
		cont, err := pool.Get(ctx)
		if err != nil {
			t.Fatalf("failed to get container %d: %v", i, err)
		}
		containers = append(containers, cont)
	}

	// verify that ports are unique
	ports := make(map[string]bool)
	ids := make(map[string]bool)

	for _, cont := range containers {
		port := cont.HostPort()
		id := cont.ID()

		if ports[port] {
			t.Errorf("duplicate port: %s", port)
		}
		if ids[id] {
			t.Errorf("duplicate container ID: %s", id)
		}

		ports[port] = true
		ids[id] = true

		t.Logf("container: id=%s, port=%s", id[:12], port)
	}

	for _, cont := range containers {
		pool.Put(cont)
	}
}

func waitForPoolSize(t *testing.T, pool *SandboxPool, expected int, timeout time.Duration) bool {
	t.Helper()
	return waitForSize(t, pool, expected, timeout)
}

func waitForPoolSizeB(b *testing.B, pool *SandboxPool, expected int, timeout time.Duration) bool {
	b.Helper()
	return waitForSize(b, pool, expected, timeout)
}

type testingTB interface {
	Helper()
	Logf(format string, args ...any)
}

func waitForSize(tb testingTB, pool *SandboxPool, expected int, timeout time.Duration) bool {
	tb.Helper()

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		if pool.Size() == expected {
			return true
		}
		<-ticker.C
	}

	tb.Logf("pool size: expected %d, got %d", expected, pool.Size())
	return false
}

func containerExists(t *testing.T, cli *client.Client, containerID string) bool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	return err == nil
}

func TestMain(m *testing.M) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		fmt.Println("docker is not available, skipping integration tests")
		os.Exit(0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ping, err := cli.Ping(ctx, client.PingOptions{})
	if err != nil {
		fmt.Printf("Docker daemon not responding: %v\n", err)
		os.Exit(0)
	}

	fmt.Printf("docker available: version %s\n", ping.APIVersion)

	_, err = cli.ImageInspect(ctx, "runtime-image:latest")
	if err != nil {
		fmt.Println("runtime-image:latest not found!")
		fmt.Println("tests will likely fail.")
	}

	code := m.Run()
	os.Exit(code)
}
