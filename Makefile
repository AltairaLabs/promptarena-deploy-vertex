.PHONY: fmt lint test test-integration build build-adapter build-runtime docker-build check install-hooks

fmt:
	GOWORK=off goimports -w -local github.com/AltairaLabs/promptarena-deploy-vertex .

lint:
	GOWORK=off golangci-lint run ./...

test:
	GOWORK=off go test ./... -race -count=1

# Deployed integration tests. These create billable GCP resources and are
# skipped unless VERTEX_TEST_PROJECT and VERTEX_TEST_IMAGE are set.
# See test/integration/README.md.
test-integration:
	GOWORK=off go test -tags=integration ./test/integration/ -v -count=1 -timeout=30m

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
