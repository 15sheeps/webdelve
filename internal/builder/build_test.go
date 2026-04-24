package builder

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidateBasic(t *testing.T) {
	cfg := DefaultConfig()
	b := NewBuilder(cfg)

	tests := []struct {
		name    string
		code    string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty code",
			code:    "",
			wantErr: true,
			errMsg:  "empty source code",
		},
		{
			name:    "valid code",
			code:    "package main\n\nfunc main() {}",
			wantErr: false,
		},
		{
			name:    "too large",
			code:    string(make([]byte, cfg.MaxSourceSize+1)),
			wantErr: true,
			errMsg:  "source code too large",
		},
		{
			name:    "forbidden import syscall",
			code:    `package main; import "syscall"; func main() {}`,
			wantErr: true,
			errMsg:  "not allowed",
		},
		{
			name:    "forbidden import unsafe",
			code:    `package main; import "unsafe"; func main() {}`,
			wantErr: true,
			errMsg:  "not allowed",
		},
		{
			name:    "not main package",
			code:    "package notmain\n\nfunc main() {}",
			wantErr: true,
			errMsg:  "package must be main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := b.ValidateBasic([]byte(tt.code))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestBuild(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BuildTimeout = 15 * time.Second
	b := NewBuilder(cfg)
	ctx := context.Background()

	validCode := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`

	tests := []struct {
		name    string
		code    string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid program",
			code:    validCode,
			wantErr: false,
		},
		{
			name:    "syntax error",
			code:    "package main\n\nfunc main() {",
			wantErr: true,
			errMsg:  "format error",
		},
		/*		{
					name:    "no package",
					code:    "func main() {}",
					wantErr: true,
					errMsg:  "package must be main",
				},
		*/{
			name:    "compilation error",
			code:    "package main\n\nfunc main() { undefinedVariable }",
			wantErr: true,
			errMsg:  "build failed",
		},
		{
			name: "multiple imports",
			code: `package main

import (
	"fmt"
	"strings"
)

func main() {
	fmt.Println(strings.ToUpper("hello"))
}
`,
			wantErr: false,
		},
		{
			name: "goroutines",
			code: `package main

import "fmt"

func main() {
	ch := make(chan string)
	go func() {
		ch <- "hello"
	}()
	fmt.Println(<-ch)
}
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := b.Build(ctx, []byte(tt.code))
			defer b.Cleanup(result)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				if result == nil {
					t.Error("result is nil")
					return
				}
				if result.ExecutablePath == "" {
					t.Error("executable path is empty")
				}
				if len(result.Formatted) == 0 {
					t.Error("formatted code is empty")
				}

				// Проверяем что бинарник существует
				info, err := os.Stat(result.ExecutablePath)
				if err != nil {
					t.Errorf("executable not found: %v", err)
				}
				if info.Size() == 0 {
					t.Error("executable is empty")
				}
				if info.Size() > int64(cfg.MaxBinarySize) {
					t.Errorf("executable size %d exceeds limit %d", info.Size(), cfg.MaxBinarySize)
				}

				// Проверяем что бинарник исполняемый
				if info.Mode()&0111 == 0 {
					t.Error("executable is not executable")
				}
			}
		})
	}
}

func TestFormatting(t *testing.T) {
	cfg := DefaultConfig()
	b := NewBuilder(cfg)
	ctx := context.Background()

	unformatted := `package main
import "fmt"
func main(){
fmt.Println("hello")
}`

	expected := `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`

	result, err := b.Build(ctx, []byte(unformatted))
	defer b.Cleanup(result)

	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	if string(result.Formatted) != expected {
		t.Errorf("formatting mismatch:\ngot:\n%s\nwant:\n%s", result.Formatted, expected)
	}
}

func TestContextCancellation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BuildTimeout = 10 * time.Second
	b := NewBuilder(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // отменяем сразу

	code := `package main

func main() {}
`

	_, err := b.Build(ctx, []byte(code))
	if err == nil {
		t.Error("expected error due to cancelled context")
	}
}

func TestConcurrentBuilds(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ConcurrentLimit = 2
	cfg.BuildTimeout = 30 * time.Second
	b := NewBuilder(cfg)

	code := `package main

func main() {}
`

	ctx := context.Background()

	// Запускаем больше сборок чем лимит
	results := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func() {
			result, err := b.Build(ctx, []byte(code))
			b.Cleanup(result)
			results <- err
		}()
	}

	// Все должны завершиться успешно
	for i := 0; i < 5; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Errorf("build %d failed: %v", i, err)
			}
		case <-time.After(60 * time.Second):
			t.Fatal("timeout waiting for builds")
		}
	}
}

func TestCleanup(t *testing.T) {
	cfg := DefaultConfig()
	b := NewBuilder(cfg)
	ctx := context.Background()

	code := `package main

func main() {}
`

	result, err := b.Build(ctx, []byte(code))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	exePath := result.ExecutablePath
	dir := filepath.Dir(exePath)

	// Проверяем что директория существует
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("build directory doesn't exist")
	}

	// Вызываем cleanup
	b.Cleanup(result)

	// Проверяем что директория удалена
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("build directory still exists after cleanup")
	}
}

func TestCleanupNil(t *testing.T) {
	cfg := DefaultConfig()
	b := NewBuilder(cfg)

	// Не должно паниковать
	b.Cleanup(nil)
}

// Вспомогательные функции

func contains(s, substr string) bool {
	if s == "" || substr == "" {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
