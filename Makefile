.PHONY: fmt lint test build docker-build check install-hooks

fmt:
	GOWORK=off goimports -w -local github.com/AltairaLabs/promptarena-deploy-vertex .

lint:
	GOWORK=off golangci-lint run ./...

test:
	GOWORK=off go test ./... -race -count=1

build:
	GOWORK=off go build -o vertex-runtime ./cmd/vertex-runtime/

docker-build:
	docker build -t promptkit-vertex-runtime:local .

check: fmt lint test build

install-hooks:
	git config core.hooksPath .githooks
