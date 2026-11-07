GO ?= go
GOCACHE ?= $(CURDIR)/.gocache
GOPATH ?= $(CURDIR)/.gopath
export GOCACHE
export GOPATH

.PHONY: test vet build bench fmt fmt-check all pip sidecar extproc openapi-diff openapi-gate acceptance smoke clean

all: fmt-check vet test

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

build:
	$(GO) build -o bin/pip ./pip/cmd/pip
	$(GO) build -o bin/sidecar ./sidecar/cmd/sidecar
	$(GO) build -o bin/extproc ./sidecar/cmd/extproc
	$(GO) build -o bin/openapi-diff ./tools/openapi-diff

bench:
	$(GO) test -run '^$$' -bench . -benchmem ./integration


fmt:
	$(GO) fmt ./...

fmt-check:
	@out=$$(gofmt -l $$(git ls-files '*.go')); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

pip:
	$(GO) run ./pip/cmd/pip -addr :8082 -fixtures pip/fixtures/attributes.json

sidecar:
	$(GO) run ./sidecar/cmd/sidecar -addr :8081 -config integration/testdata/config -pip http://127.0.0.1:8082

extproc:
	$(GO) run ./sidecar/cmd/extproc -listen :50051 -config integration/testdata/config -pip http://127.0.0.1:8082

openapi-diff:
	$(GO) run ./tools/openapi-diff -openapi $(OPENAPI) -registry $(REGISTRY)

openapi-gate:
	$(GO) run ./tools/openapi-diff -openapi integration/testdata/openapi.json -registry integration/testdata/config/registry.json

# BDD acceptance criteria against the in-process engine. Set
# CONTROLS_ACCEPTANCE_URL to run the same features against a live HTTP entrypoint
# (see `make smoke`, which points it at the Envoy listener).
acceptance:
	$(GO) test -run TestAcceptance -count=1 -v ./integration

smoke:
	./hack/envoy-smoke.sh

clean:
	rm -rf bin .gocache .gopath
