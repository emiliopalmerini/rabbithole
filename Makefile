.PHONY: all fmt check vet build run test clean help

BINARY_NAME := rabbithole

all: fmt vet test build

fmt:
	go fmt ./...

check:
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "Files not formatted:"; \
		gofmt -l .; \
		exit 1; \
	fi

vet: fmt
	go vet ./...

build: vet
	go build -o $(BINARY_NAME) .

build-release: fmt
	go build -ldflags="-s -w" -o $(BINARY_NAME) .

run: build
	./$(BINARY_NAME)

test: vet
	go test -v ./...

test-coverage: vet
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
	go clean ./...

help:
	@echo "rabbithole - TUI for consuming and inspecting RabbitMQ messages"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Build targets:"
	@echo "  all           - Format, vet, test, and build (default)"
	@echo "  build         - Build the rabbithole binary"
	@echo "  build-release - Build optimized binary"
	@echo "  clean         - Remove build artifacts"
	@echo ""
	@echo "Code quality:"
	@echo "  fmt           - Format code"
	@echo "  check         - Check formatting (no changes)"
	@echo "  vet           - Run go vet"
	@echo ""
	@echo "Testing:"
	@echo "  test          - Run tests"
	@echo "  test-coverage - Run tests with coverage report"
	@echo ""
	@echo "Run:"
	@echo "  run           - Build and run rabbithole"
