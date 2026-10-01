# uspace-core developer targets. CI (.github/workflows/ci.yml) runs the
# same commands. On Windows set GOROOT and GO, for example:
#   make test GO=/c/Users/<you>/AppData/Local/anaconda3/bin/go
GO      ?= go
PKGS    ?= ./...
FUZZTIME ?= 10s
LAB     ?= ../uspace-lab

# Linter versions pinned to the ones .github/workflows/ci.yml runs. Change
# both files together. `make tools` installs them into $(go env GOPATH)/bin.
GOLANGCI_LINT_VERSION ?= v2.14.0
STATICCHECK_VERSION   ?= v0.8.1

.PHONY: all build vet fmt fmt-check lint tools staticcheck test race cover vectors \
        check-vectors sync-vectors fuzz-smoke bench tidy ci clean

all: ci

build:
	$(GO) build $(PKGS)

vet:
	$(GO) vet $(PKGS)

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

tools:
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	$(GO) install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) $(PKGS)

# Refuses to run a golangci-lint other than the pinned one: a different
# version enables different checks and would pass locally but fail in CI.
lint: fmt-check vet staticcheck
	@v="v$$(golangci-lint version --short 2>/dev/null)"; 	if [ "$$v" != "$(GOLANGCI_LINT_VERSION)" ]; then 	  echo "golangci-lint $$v found, CI runs $(GOLANGCI_LINT_VERSION): run 'make tools'"; exit 1; fi
	golangci-lint run $(PKGS)

tidy:
	$(GO) mod tidy
	git diff --exit-code -- go.mod go.sum

test:
	$(GO) test -count=1 -shuffle=on $(PKGS)

race:
	$(GO) test -race -count=1 -shuffle=on $(PKGS)

cover:
	$(GO) test -count=1 -coverprofile=coverage.out -covermode=atomic $(PKGS)
	$(GO) tool cover -func=coverage.out | tail -n 1

# The knowledge vectors: harness manifest and every package's vector test.
vectors:
	$(GO) test -count=1 -run 'Vector|Manifest|Version' -v $(PKGS)

# Offline sha256 check plus, when reachable, the diff against uspace-lab at
# the commit in vectors/testdata/VERSION.
check-vectors:
	scripts/check-vectors.sh

# Refresh the vendored copy from a clean uspace-lab checkout ($(LAB)).
sync-vectors:
	scripts/sync-vectors.sh $(LAB)

fuzz-smoke:
	FUZZTIME=$(FUZZTIME) GO=$(GO) scripts/fuzz-smoke.sh

bench:
	$(GO) test -run '^$$' -bench . -benchmem -count=1 $(PKGS) | tee bench.txt
	scripts/bench-report.sh bench.txt

ci: build vet lint race vectors check-vectors fuzz-smoke

clean:
	rm -f coverage.out bench.txt
