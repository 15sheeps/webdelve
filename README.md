# webdelve - Online Go Compiler & Debugger

webdelve is a web service that provides an online debugging environment for Go programs. It allows to run and debug Go code directly from browser using the [delve](https://github.com/go-delve/delve) server running in isolated docker containers.

## Demo

https://github.com/user-attachments/assets/65cf928e-e203-4de4-aa1d-324ec2d01575

## Features

- **web-based debugging** - debug go code from any browser
- **prometheus metrics** - monitor performance and usage
- **websocket communication** - real-time debugger interaction
- **code formatting** - automatic go code formatting

## Supported debug Commands

| Command | Description | Arguments |
|---------|-------------|-----------|
| `continue` | Resume execution | - |
| `next` | Step over | - |
| `step` | Step into | - |
| `stepout` | Step out | - |
| `break` | Set breakpoint | `file`, `line` |
| `clear` | Remove breakpoint | `id` |
| `stack` | Show stack trace | `goroutineId`, `depth` |
| `locals` | Show local variables | `goroutineId`, `frame` |
| `goroutines` | List all goroutines | - |
| `state` | Get debugger state | - |
| `restart` | Restart program | - |

## Prerequisites

- **Docker** with **gVisor runtime** (runsc)
- **Make** (optional)

## Usage

### Option 1: Docker (recommended)

Builds both images (for sandbox and service), then runs everything in containers.

```bash
make docker-run
```

Service will be available at address specified in `config.yaml`.  
Stop with `make docker-stop`.

### Option 2: Local development (service on host, sandbox in Docker)

Requires the sandbox Docker image to be built once.

```bash
# Build the sandbox image
docker build -f Dockerfile.sandbox -t webdelve-sandbox:latest .

# Build and run the service binary locally
make run
```

The service will use your local Docker daemon to create sandbox containers from the `webdelve-sandbox:latest` image.

## API Usage

### Start a debugging session

```bash
curl -X POST http://localhost:8080/build \
  -H "Content-Type: application/json" \
  -d '{"code": "package main\n\nfunc main() {\n    println(\"Hello\")\n}"}'
```

Response:
```json
{
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "formatted": "package main\n\nfunc main() {\n\tprintln(\"Hello\")\n}\n"
}
```

### Connect via WebSocket

```javascript
const ws = new WebSocket("ws://localhost:8080/ws/550e8400...");

ws.send(JSON.stringify({ command: "continue" }));
ws.send(JSON.stringify({ command: "break", args: { file: "main.go", line: 4 } }));
```

## Metrics

Prometheus metrics available at `http://localhost:8080/metrics`:

- `http_requests_total`
- `build_duration_seconds`
- `build_errors_total`
- `sandbox_idle`
- `active_sessions`
- `debug_command_duration_seconds`

## Future Improvements

- **Multi‑file programs** – Allow uploading multiple `.go` files. (or [txtar format](https://pkg.go.dev/golang.org/x/tools/txtar)).
- **Rate limiting** – Per‑IP limits on build requests and debug commands.
- **Improved frontend** - Current frontend uses pure js and therefore hard to mantain
