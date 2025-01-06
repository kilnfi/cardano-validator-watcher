.PHONY: generate
generate:
	@mockery

.PHONY: build
build: generate
	@go build  -ldflags="-s -w" -o bin/cardano-validator-watcher cmd/watcher/main.go

.PHONY: run
run: generate
	@go run cmd/watcher/main.go --config config.yaml

.PHONY: tests
tests:
	@go test -v ./...

.PHONY: coverage
coverage:
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out

.PHONY: lint
lint:
	@golangci-lint run ./...

.PHONY: clean
clean:
	@echo "cleaning up..."
	@rm -rf *.db*