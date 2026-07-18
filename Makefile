VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build test vet cross clean

build:
	go build $(LDFLAGS) -o notraker ./cmd/notraker

test:
	go test ./...

vet:
	go vet ./...

cross:
	GOOS=linux   GOARCH=amd64 go build $(LDFLAGS) -o dist/notraker-linux-amd64       ./cmd/notraker
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/notraker-windows-amd64.exe ./cmd/notraker
	GOOS=darwin  GOARCH=arm64 go build $(LDFLAGS) -o dist/notraker-darwin-arm64      ./cmd/notraker
	GOOS=darwin  GOARCH=amd64 go build $(LDFLAGS) -o dist/notraker-darwin-amd64      ./cmd/notraker

clean:
	rm -rf notraker notraker.exe dist
