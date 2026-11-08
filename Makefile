GO ?= go
HELM ?= helm
HELM_ARGS ?=
CONTROLS_NAMESPACE ?= controls
LOADTEST_URL ?= http://127.0.0.1:18000/accounts/A123
LOADTEST_ARGS ?=
BUNDLE_DIR ?= integration/testdata/config
PIP_FIXTURES ?= pip/fixtures/attributes.json
API_ADDR ?= 127.0.0.1:8081
ENV ?= prod
SERVICE ?= accounts
GOCACHE ?= $(CURDIR)/.gocache
GOPATH ?= $(CURDIR)/.gopath
export GOCACHE
export GOPATH

.PHONY: test vet build bench fmt fmt-check all pip sidecar api api-rules extproc openapi-diff openapi-gate acceptance smoke testdrive demo examples steward clean pip-contract pip-api-compat

all: fmt-check vet test

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

vulncheck:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

build:
	$(GO) build -o bin/pip ./pip/cmd/pip
	$(GO) build -o bin/sidecar ./sidecar/cmd/sidecar
	$(GO) build -o bin/extproc ./sidecar/cmd/extproc
	$(GO) build -o bin/openapi-diff ./tools/openapi-diff
	$(GO) build -o bin/openapi-registry ./tools/openapi-registry
	$(GO) build -o bin/controls-check ./tools/controls-check
	$(GO) build -o bin/bundle ./tools/bundle
	$(GO) build -o bin/conformance ./tools/conformance
	$(GO) build -o bin/bundle-sign ./tools/bundle-sign
	$(GO) build -o bin/bundle-keygen ./tools/bundle-keygen

bench:
	$(GO) test -run '^$$' -bench . -benchmem ./integration


fmt:
	$(GO) fmt ./...

fmt-check:
	@out=$$(gofmt -l $$(git ls-files '*.go')); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

pip:
	$(GO) run ./pip/cmd/pip -addr 127.0.0.1:8082 -metrics-addr 127.0.0.1:9091 -fixtures $(PIP_FIXTURES)

# External-PIP harness: run `make pip` in another terminal first.
sidecar:
	$(GO) run ./sidecar/cmd/sidecar -addr $(API_ADDR) -config $(BUNDLE_DIR) -pip http://127.0.0.1:8082

# One process: test API plus an in-process PIP backed by fixtures.
api:
	$(GO) run ./sidecar/cmd/sidecar -addr $(API_ADDR) -config $(BUNDLE_DIR) -fixtures $(PIP_FIXTURES)

# Render the delivery-owned rules, then serve that bundle on the test API.
api-rules:
	$(GO) run ./tools/bundle -src ../controls-rules -env $(ENV) -service $(SERVICE) -out dist/$(SERVICE)
	$(GO) run ./sidecar/cmd/sidecar -addr $(API_ADDR) -config dist/$(SERVICE) -fixtures $(PIP_FIXTURES)

extproc:
	CONTROLS_AUDIT_SALT=dev-salt $(GO) run ./sidecar/cmd/extproc -listen :50051 -config integration/testdata/config -pip http://127.0.0.1:8082

openapi-diff:
	$(GO) run ./tools/openapi-diff -openapi $(OPENAPI) -registry $(REGISTRY)

openapi-gate:
	$(GO) run ./tools/openapi-diff -openapi integration/testdata/openapi.json -registry integration/testdata/config/registry.json

# BDD acceptance criteria against the in-process engine. Set
# CONTROLS_ACCEPTANCE_URL to run the same features against a live HTTP entrypoint
# (see `make smoke`, which points it at the Envoy listener).
acceptance:
	$(GO) test -run TestAcceptance -count=1 -v ./integration

pip-contract:
	$(GO) test ./pip/server -run TestServerMatchesPublishedContract -count=1

pip-api-compat:
	./hack/pip-api-compat.sh $${BASE_REF:-HEAD~1}

smoke:
	./hack/envoy-smoke.sh

testdrive:
	./hack/testdrive.sh

demo:
	CONTROLS_DEMO=1 ./hack/envoy-smoke.sh

examples:
	$(MAKE) -C examples/accounts-read test
	$(MAKE) -C examples/payments-write test
	$(MAKE) -C examples/pip-outage test
	$(MAKE) -C examples/mtls-caller test
	$(MAKE) -C examples/spring-accounts test

steward:
	$(GO) run ./tools/steward -root .

version-check:
	@test -n "$$(cat VERSION)" && echo "version $$(cat VERSION)"

loadtest:
	$(GO) run ./hack/loadtest -url $(LOADTEST_URL) \
		-H 'x-subject: u-1' -H 'x-claim-region: US' $(LOADTEST_ARGS)

chart:
	$(HELM) lint deploy/helm/controls-plane
	$(HELM) template controls deploy/helm/controls-plane --namespace controls >/dev/null

cluster-controls:
	mkdir -p dist
	$(HELM) template controls deploy/helm/controls-plane --namespace $(CONTROLS_NAMESPACE) $(HELM_ARGS) > dist/cluster-controls.yaml
	@echo "rendered dist/cluster-controls.yaml"

# Render the sibling delivery repo for local inspection (not part of CI).
render-rules:
	$(GO) run ./tools/bundle -src ../controls-rules -env $(ENV) -service $(SERVICE) -out dist/$(SERVICE)
	$(GO) run ./tools/conformance -bundle dist/$(SERVICE) -fixtures ../controls-rules/tests/fixtures -service $(SERVICE) -v

clean:
	rm -rf bin .gocache .gopath
