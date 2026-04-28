package builder

import (
	"github.com/15sheeps/webdelve/internal/config"
	"github.com/15sheeps/webdelve/internal/sandbox"

	"go/ast"
	"go/format"
	"go/parser"
	"go/token"

	"bytes"
	"context"
	"errors"
	"fmt"
	"golang.org/x/sync/semaphore"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

type BuildResult struct {
	Formatted string
	ExePath   string
}

type Builder struct {
	cfg config.BuilderConfig
	sem *semaphore.Weighted
}

func NewBuilder(cfg config.BuilderConfig) *Builder {
	if cfg.ConcurrentLimit <= 0 {
		cfg.ConcurrentLimit = 1
	}

	return &Builder{
		cfg: cfg,
		sem: semaphore.NewWeighted(cfg.ConcurrentLimit),
	}
}

// verify that package is named "main" and "main" function exists
func validateMain(src []byte) error {
	// parsing AST
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	if f.Name.Name != "main" {
		return errors.New("package must be main")
	}

	hasMainFunc := false
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		// looking for "main" func that shouldn't have receiver
		if ok && fn.Name.Name == "main" && fn.Recv == nil {
			// also shouldn't have parameters or results
			if fn.Type.Params.NumFields()+fn.Type.Results.NumFields() == 0 {
				hasMainFunc = true
				break
			}
		}
	}

	if !hasMainFunc {
		return errors.New("main function isn't declared in the package")
	}

	return nil
}

// Build formats provided source code and builds binary
func (b *Builder) Build(pctx context.Context, src []byte) (*BuildResult, error) {
	ctx, cancel := context.WithTimeout(pctx, b.cfg.BuildTimeout)
	defer cancel()

	if err := b.sem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer b.sem.Release(1)

	if err := validateMain(src); err != nil {
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
	goModContent := []byte("module main\n\ngo 1.23\n")
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

	return &BuildResult{
		ExePath:   exePath,
		Formatted: string(formatted),
	}, nil
}

func (b *Builder) build(ctx context.Context, dir, output string) error {
	cmd := exec.CommandContext(ctx,
		"go", "build",
		"-o", output,
//		"-tags=faketime",
		"-gcflags=all=-N -l", ".", // delve requires '-N -l' flags
	)

	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GOROOT=/usr/local/go",
		"GOOS=linux",
		"GOARCH=amd64",
		"CGO_ENABLED=0",
		"GOPROXY=off", // allow only stdlib for now
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("build timeout exceeded (%v)", b.cfg.BuildTimeout)
		}
		return fmt.Errorf("build failed: %s", stderr.String())
	}

	fileInfo, err := os.Stat(output)
	switch {
	case err != nil:
		return fmt.Errorf("failed to stat binary: %w", err)
	case fileInfo.Size() == 0:
		return errors.New("built binary is empty")
	case fileInfo.Size() > b.cfg.MaxBinarySize:
		return fmt.Errorf("built binary size exceeds limit (%d)", b.cfg.MaxBinarySize)
	}

	return nil
}

// Clenup removes temporary directory after build
func Cleanup(result *BuildResult) {
	if result != nil && result.ExePath != "" {
		dir := filepath.Dir(result.ExePath)
		os.RemoveAll(dir)
	}
}

// Upload transfers built binary into the cointainer
func Upload(ctx context.Context, container *sandbox.Container, exePath string) error {
	// read the binary
	data, err := os.ReadFile(exePath)
	if err != nil {
		return fmt.Errorf("failed to read binary: %w", err)
	}

	// copy binary to the container
	if err := container.CopyFileTo(ctx, "/program", data); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	// chmod +x
	reader, err := container.Exec(ctx, []string{"chmod", "+x", "/program"})
	if err != nil {
		return fmt.Errorf("failed to chmod binary: %w", err)
	}
	io.Copy(io.Discard, reader)

	return nil
}
