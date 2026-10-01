# uspace-core developer targets. CI (.github/workflows/ci.yml) runs the
# same commands. On Windows set GOROOT and GO, for example:
#   make test GO=/c/Users/<you>/AppData/Local/anaconda3/bin/go
GO      ?= go
PKGS    ?= ./...
FUZZTIME ?= 10s
LAB     ?= ../uspace-lab

.PHONY: all build vet fmt fmt-check lint staticcheck test race cover vectors \
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

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@latest $(PKGS)

lint: fmt-check vet staticcheck
	golangci-lint run

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
