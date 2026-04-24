package builder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/15sheeps/webdelve/internal/config"
	"github.com/15sheeps/webdelve/internal/manager"
	"github.com/15sheeps/webdelve/internal/sandbox"
	"go/format"
	"go/parser"
	"go/token"
	"golang.org/x/sync/semaphore"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Builder struct {
	cfg     config.BuilderConfig
	pool    *sandbox.SandboxPool
	manager *manager.Manager
	sem     *semaphore.Weighted
}

func NewBuilder(
	cfg config.BuilderConfig,
	pool *sandbox.SandboxPool,
	manager *manager.Manager,
) *Builder {
	if cfg.ConcurrentLimit <= 0 {
		cfg.ConcurrentLimit = 1
	}

	return &Builder{
		pool:    pool,
		cfg:     cfg,
		manager: manager,
		sem:     semaphore.NewWeighted(cfg.ConcurrentLimit),
	}
}

func (b *Builder) ValidateBasic(src []byte) error {
	// validating imports
	// TODO: make it able to use these packages or at least parse source instead of string.Contains
	forbiddenImports := []string{
		"os/exec",
		"syscall",
		"unsafe",
		"net/http",
	}

	for _, imp := range forbiddenImports {
		if strings.Contains(string(src), fmt.Sprintf("%q", imp)) {
			return fmt.Errorf("import %q is not allowed", imp)
		}
	}
	// check if package declared main
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.PackageClauseOnly)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}
	if f.Name.Name != "main" {
		return errors.New("package must be main")
	}
	return nil
}

func (b *Builder) Build(ctx context.Context, src []byte) (*BuildResponse, error) {
	if err := b.sem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer b.sem.Release(1)

	if err := b.ValidateBasic(src); err != nil {
		return nil, err
	}

	formatted, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("format error: %w", err)
	}
	// make temp directory build-XXXXXXXXX
	tmpDir, err := os.MkdirTemp("", "build-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	// place main.go in that directory
	mainPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainPath, formatted, 0644); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to write main.go: %w", err)
	}
	// create go.mod
	goModContent := []byte("module main\n")
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), goModContent, 0644); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to write go.mod: %w", err)
	}
	// finally build the program
	exePath := filepath.Join(tmpDir, "program")
	if err := b.build(ctx, tmpDir, exePath); err != nil {
		os.RemoveAll(tmpDir)
		return nil, err
	}

	return &BuildResponse{
		exePath:   exePath,
		Formatted: string(formatted),
	}, nil
}

func (b *Builder) build(ctx context.Context, dir, output string) error {
	buildCtx, cancel := context.WithTimeout(ctx, b.cfg.BuildTimeout)
	defer cancel()

	cmd := exec.CommandContext(buildCtx, "go", "build", "-o", output, "-gcflags=all=-N -l", "main.go")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH=amd64",
		"CGO_ENABLED=0",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if buildCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("build timeout exceeded (%v)", b.cfg.BuildTimeout)
		}
		return fmt.Errorf("build failed: %s", stderr.String())
	}

	return nil
}

// Clenup removes temporary directory with executable in it
func Cleanup(result *BuildResponse) {
	if result != nil && result.exePath != "" {
		dir := filepath.Dir(result.exePath)
		os.RemoveAll(dir)
	}
}

func Upload(ctx context.Context, container *sandbox.Container, exePath string) error {
	// read the binary
	data, err := os.ReadFile(exePath)
	if err != nil {
		return fmt.Errorf("failed to read binary: %w", err)
	}

	// copy binary to the container
	if err := container.CopyFileTo(ctx, "/sandbox/program", data); err != nil {
		return fmt.Errorf("failed to copy binary to container: %w", err)
	}

	// chmod +x
	reader, err := container.Exec(ctx, []string{"chmod", "+x", "/sandbox/program"})
	if err != nil {
		return fmt.Errorf("failed to chmod binary: %w", err)
	}
	go io.Copy(io.Discard, reader)

	return nil
}
