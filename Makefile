.PHONY: build build-mcp run clean dev test lint benchmark

# Binary output
BINARY := bin/purify

# Build flags
LDFLAGS := -s -w

build:
	@echo "Building purify..."
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/purify

build-mcp:
	@echo "Building purify-mcp..."
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/purify-mcp ./cmd/purify-mcp

run: build
	@echo "Starting purify..."
	./$(BINARY)

dev:
	@echo "Starting purify in dev mode..."
	PURIFY_MODE=debug PURIFY_AUTH_ENABLED=false PURIFY_MAX_PAGES=5 go run ./cmd/purify

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/
	go clean

tidy:
	go mod tidy

benchmark:
	@echo "Running Purify benchmark suite..."
	@echo "Make sure Purify is running at localhost:8080"
	@echo ""
	go run ./scripts/benchmark/main.go
