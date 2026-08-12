.PHONY: fmt lint test build build-adapter build-runtime docker-build check install-hooks

fmt:
	GOWORK=off goimports -w -local github.com/AltairaLabs/promptarena-deploy-vertex .

lint:
	GOWORK=off golangci-lint run ./...

test:
	GOWORK=off go test ./... -race -count=1

build: build-adapter build-runtime

build-adapter:
	GOWORK=off go build -o promptarena-deploy-vertex .

build-runtime:
	GOWORK=off go build -o vertex-runtime ./cmd/vertex-runtime/

docker-build:
	docker build -t promptkit-vertex-runtime:local .

check: fmt lint test build

install-hooks:
	git config core.hooksPath .githooks
