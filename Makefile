BIN_DIR ?= $(HOME)/.local/bin
GO_IMAGE ?= golang:1.23-alpine
HOST_UID ?= $(shell id -u)
HOST_GID ?= $(shell id -g)
GO_DOCKER = docker run --rm --user "$(HOST_UID):$(HOST_GID)" --env HOME=/tmp --env GOCACHE=/src/.cache/go-build --env GOMODCACHE=/src/.cache/go-mod --env CGO_ENABLED=0 --env GOOS=linux --env GOARCH=amd64 --volume "$(CURDIR):/src" --workdir /src $(GO_IMAGE)

.PHONY: prepare fmt test build dev
prepare:
	@mkdir -p bin .cache/go-build .cache/go-mod
fmt: prepare
	$(GO_DOCKER) go fmt ./...
test: prepare
	$(GO_DOCKER) go test ./...
build: prepare
	$(GO_DOCKER) go build -trimpath -o bin/laradev ./cmd/laradev
dev: build
	./bin/laradev install --bin-dir "$(BIN_DIR)"
