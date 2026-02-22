VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build install test lint clean

build:
	go build $(LDFLAGS) -o ferry ./cmd/ferry/

install:
	go install $(LDFLAGS) ./cmd/ferry/

test:
	go test ./... -v

lint:
	golangci-lint run ./...

clean:
	rm -f ferry
