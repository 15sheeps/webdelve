.PHONY: help build run clean test docker-build docker-run docker-stop

BIN_DIR := ./bin
SERVICE_BIN := $(BIN_DIR)/service
CONFIG_PATH := ./config.yaml
CONTAINER_NAME := webdelve-service

help:
	@echo "Available commands:"
	@echo "  make build          - Build service binary only"
	@echo "  make run            - Run service locally (requires sandbox Docker image)"
	@echo "  make clean          - Remove build artifacts"
#	@echo "  make test           - Run tests"
	@echo "  make docker-build   - Build both sandbox and service Docker images"
	@echo "  make docker-run     - Run service container (with Docker)"
	@echo "  make docker-stop    - Stop and remove service container"

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(SERVICE_BIN) ./cmd/service

run: build
	CONFIG_PATH=$(CONFIG_PATH) $(SERVICE_BIN)

clean:
	rm -rf $(BIN_DIR)
	go clean -cache

#test:
#	go test -v -race ./...

docker-build:
	docker build -f Dockerfile.sandbox -t webdelve-sandbox:latest .
	docker build -f Dockerfile.service -t webdelve-service:latest .

docker-run: docker-build
	docker rm -f $(CONTAINER_NAME) 2>/dev/null || true
	docker run -d --name $(CONTAINER_NAME) \
		-p 8080:8080 \
		-e CONFIG_PATH=/config/config.yaml \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v $(PWD)/config.yaml:/config/config.yaml:ro \
		webdelve-service:latest

docker-stop:
	docker stop $(CONTAINER_NAME) 2>/dev/null || true # sends SIGTERM for graceful shutdown in /cmd/service
	docker rm $(CONTAINER_NAME) 2>/dev/null || true
	docker rm -f $(shell docker ps -aq --filter "ancestor=webdelve-sandbox:latest") 2>/dev/null || true

.DEFAULT_GOAL := help
