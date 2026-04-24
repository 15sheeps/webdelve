package main

import (
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/go-delve/delve/service"
	"github.com/go-delve/delve/service/debugger"
	"github.com/go-delve/delve/service/rpccommon"
)

const (
	watchDir    = "."       // directory to watch
	programName = "program" // executable name
	listenAddr  = ":2345"   // delve listening address
)

func main() {
	// wait for executable in the container work directory
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}
	defer watcher.Close()

	if err := watcher.Add(watchDir); err != nil {
		panic(err)
	}

	targetPath := filepath.Join(watchDir, programName)

watchLoop:
	for {
		select {
		case event := <-watcher.Events:
			if event.Op&fsnotify.Chmod == fsnotify.Chmod {
				if filepath.Base(event.Name) == programName {
					break watchLoop
				}
			}
		case err := <-watcher.Errors:
			panic(err)
		}
	}
	println("starting delve server...")
	// start delve server
	// dlv exec ./program --listen=addr --headless --api-version=2 --accept-multiclient
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		panic(err)
	}

	server := rpccommon.NewServer(&service.Config{
		Listener:    listener,
		AcceptMulti: true,
		APIVersion:  2,
		ProcessArgs: []string{targetPath},
		Debugger: debugger.Config{
			WorkingDir:  filepath.Dir(watchDir),
			Backend:     "default",
			ExecuteKind: debugger.ExecutingExistingFile, // dlv exec
		},
	})

	if err := server.Run(); err != nil {
		listener.Close()
		panic(err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
}
