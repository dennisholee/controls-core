ROOT := ../..
IMAGE := controls-sidecar:dev
GO ?= go
DOCKER_BUILDKIT ?= 0
GOARCH ?= $(shell go env GOARCH)
TMP := $(abspath $(ROOT)/.tmp/sample)

.PHONY: gate up test down

up:
	mkdir -p $(TMP)
	cd $(ROOT) && CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -o $(TMP)/extproc ./sidecar/cmd/extproc
	cd $(ROOT) && CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -o $(TMP)/pip ./pip/cmd/pip
	DOCKER_BUILDKIT=$(DOCKER_BUILDKIT) docker build --build-arg EXTPROC_BIN=.tmp/sample/extproc -f $(ROOT)/deploy/sidecar/Dockerfile -t $(IMAGE) $(ROOT)
	DOCKER_BUILDKIT=$(DOCKER_BUILDKIT) docker build -f app/Dockerfile -t accounts-read-app:dev app
	DOCKER_BUILDKIT=$(DOCKER_BUILDKIT) docker build --build-arg PIP_BIN=.tmp/sample/pip -f pip/Dockerfile -t accounts-read-pip:dev $(ROOT)
	docker compose -f compose.yaml up -d

gate:
	cd $(ROOT) && $(GO) run ./tools/openapi-diff \
		-openapi examples/accounts-read/openapi.yaml \
		-registry examples/accounts-read/controls/registry.json

test: gate up
	./test.sh

down:
	docker compose -f compose.yaml down -v
