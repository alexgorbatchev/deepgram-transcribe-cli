set positional-arguments := true

default: build

# Build the deepgram-transcribe binary into bin/
build:
	mkdir -p bin
	go build -o bin/deepgram-transcribe ./cmd/deepgram-transcribe

# Run the deepgram-transcribe CLI binary with any arguments
# Usage: just run "/path/with spaces/file.m4a"
# Usage: just run history
run *args: build
	./bin/deepgram-transcribe "$@"

# View transcription history
# Usage: just history
history *args: build
	./bin/deepgram-transcribe history "$@"

# View post-transcription cost & metadata for a file or request ID
# Usage: just cost "/path/with spaces/file.m4a"
cost *args: build
	./bin/deepgram-transcribe cost "$@"

# Run unit tests
test:
	go test -v ./...

# Run unit tests and calculate coverage
coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

# Clean build artifacts
clean:
	rm -rf bin/ coverage.out coverage.html
