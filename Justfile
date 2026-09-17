set positional-arguments := true

binary_name := "deepgram-transcribe"

default: build

# Build the binary into bin/
build:
	mkdir -p bin
	go build -o bin/{{binary_name}} ./cmd/{{binary_name}}

# Run the CLI in human mode
# Usage: just run transcript create "/path/with spaces/file.m4a"
run *args: build
	./bin/{{binary_name}} "$@"

# Run the CLI in agent mode, which trades formatting for fewer tokens
# Usage: just run-ai job list
run-ai *args: build
	AGENT=1 ./bin/{{binary_name}} "$@"

# Run unit tests
test:
	go test -race ./...

# Check formatting and run static analysis
lint:
	gofmt -l .
	go vet ./...
	golangci-lint run

# Run the full pre-commit gate
check: lint test
	go mod tidy -diff

# Run unit tests and report coverage
coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

# Remove build artifacts
clean:
	rm -rf bin/ coverage.out coverage.html
