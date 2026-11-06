GO ?= go
GOCACHE ?= $(CURDIR)/.gocache
GOPATH ?= $(CURDIR)/.gopath
export GOCACHE
export GOPATH

.PHONY: test vet build fmt fmt-check all pip sidecar clean

all: fmt-check vet test

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

build:
	$(GO) build -o bin/pip ./pip/cmd/pip
	$(GO) build -o bin/sidecar ./sidecar/cmd/sidecar

fmt:
	gofmt -w .

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

pip:
	$(GO) run ./pip/cmd/pip -addr :8082 -fixtures pip/fixtures/attributes.json

sidecar:
	$(GO) run ./sidecar/cmd/sidecar -addr :8081 -config integration/testdata/config -pip http://127.0.0.1:8082

clean:
	rm -rf bin .gocache .gopath
