.PHONY: all build test clean run

BINARY_NAME=remgo

all: test build

build:
	@echo "==> Building $(BINARY_NAME)..."
	go build -ldflags="-s -w" -o $(BINARY_NAME) ./cmd/remgo

test:
	@echo "==> Running tests..."
	go test -v -race ./...

run: build
	./$(BINARY_NAME)

clean:
	@echo "==> Cleaning..."
	rm -f $(BINARY_NAME)
	rm -rf remgo_data/
