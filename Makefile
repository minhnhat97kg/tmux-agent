.PHONY: build test fmt vet clean update-frames

BIN := bin/tmux-agent

build:
	go build -o $(BIN) ./cmd/tmux-agent

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

# Re-capture every golden frame. Only run after checking the frames by eye.
update-frames: build
	UPDATE_FRAMES=1 go test ./internal/frametest/... -v

clean:
	rm -rf bin
