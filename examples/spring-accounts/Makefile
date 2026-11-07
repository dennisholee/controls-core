ROOT := ../..
IMAGE := controls-sidecar:dev
GO ?= go
DOCKER_BUILDKIT ?= 0
GOARCH ?= $(shell go env GOARCH)
TMP := $(abspath $(ROOT)/.tmp/sample)

.PHONY: gate up test down

up:
	mvn -o -q -DskipTests package
	mkdir -p $(TMP)
	cd $(ROOT) && CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -o $(TMP)/extproc ./sidecar/cmd/extproc
	cd $(ROOT) && CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -o $(TMP)/pip ./pip/cmd/pip
	DOCKER_BUILDKIT=$(DOCKER_BUILDKIT) docker build --build-arg EXTPROC_BIN=.tmp/sample/extproc -f $(ROOT)/deploy/sidecar/Dockerfile -t $(IMAGE) $(ROOT)
	DOCKER_BUILDKIT=$(DOCKER_BUILDKIT) docker build -f Dockerfile -t spring-accounts-app:dev .
	DOCKER_BUILDKIT=$(DOCKER_BUILDKIT) docker build --build-arg PIP_BIN=.tmp/sample/pip -f pip/Dockerfile -t spring-accounts-pip:dev $(ROOT)
	docker compose -f compose.yaml up -d

gate:
	cd $(ROOT) && $(GO) run ./tools/openapi-diff \
		-openapi examples/spring-accounts/openapi.json \
		-registry examples/spring-accounts/controls/registry.json

test: gate up
	./test.sh

down:
	docker compose -f compose.yaml down -v
