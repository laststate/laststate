BINARY := bin/laststate-relay
VERSION ?= dev
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.cliVersion=$(VERSION) -X main.gitCommit=$(COMMIT) -X main.buildDate=$(DATE)

.PHONY: fmt test race vet build clean dist
fmt:
	gofmt -w cmd internal

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

build:
	mkdir -p bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/laststate-relay

dist:
	mkdir -p dist
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/laststate-relay-linux-amd64 ./cmd/laststate-relay
	GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/laststate-relay-linux-arm64 ./cmd/laststate-relay
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/laststate-relay-windows-amd64.exe ./cmd/laststate-relay
	GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/laststate-relay-darwin-amd64 ./cmd/laststate-relay
	GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/laststate-relay-darwin-arm64 ./cmd/laststate-relay

clean:
	rm -rf bin dist coverage.out
