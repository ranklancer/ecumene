# Juridical build + gate. `make gate-full` is the single source of truth for
# the quality bar; CI runs the same target so local and CI stay identical.

GO            ?= go
COVER_FLOOR   ?= 74
BINARY        ?= ecumene
PKG           ?= ./...
VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)
COMMIT        ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE          ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS       := -s -w \
	-X github.com/ranklancer/ecumene/internal/version.Version=$(VERSION) \
	-X github.com/ranklancer/ecumene/internal/version.Commit=$(COMMIT) \
	-X github.com/ranklancer/ecumene/internal/version.Date=$(DATE)

.PHONY: all gate-full fmt vet build test cover lint gosec gitleaks pii smoke fuzz tools clean

all: build

fmt:
	$(GO) fmt $(PKG)
	@test -z "$$(gofmt -l cmd internal 2>/dev/null)" || { echo "gofmt: files need formatting"; gofmt -l cmd internal; exit 1; }

vet:
	$(GO) vet $(PKG)

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/ecumene

test:
	$(GO) test -race -count=1 $(PKG)

cover:
	@$(GO) test -covermode=atomic -coverprofile=cover.out $(PKG) >/dev/null
	@pct=$$($(GO) tool cover -func=cover.out | awk '/^total:/ {gsub(/%/,"",$$3); print $$3}'); \
	echo "total coverage: $$pct% (floor $(COVER_FLOOR)%)"; \
	awk -v p="$$pct" -v f="$(COVER_FLOOR)" 'BEGIN { exit (p+0 >= f+0) ? 0 : 1 }' || { echo "coverage below floor"; exit 1; }

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run $(PKG); else echo "golangci-lint not installed; skipping (required in CI)"; fi

gosec:
	@if command -v gosec >/dev/null 2>&1; then gosec -quiet ./...; else echo "gosec not installed; skipping (required in CI)"; fi

gitleaks:
	@if command -v gitleaks >/dev/null 2>&1; then gitleaks detect --source . --redact --no-banner --exit-code 1; else echo "gitleaks not installed; skipping (required in CI)"; fi

pii:
	./tools/pii_scan.sh

smoke: build
	./$(BINARY) -version >/dev/null
	./$(BINARY) doctrine >/dev/null
	./$(BINARY) forge nginx@sha256:0000000000000000000000000000000000000000000000000000000000000000 >/dev/null

fuzz:
	@echo "no fuzz targets in the founding scaffold; fuzz untrusted-input parsers as they land (the internal design spec)"

# Full local gate, mirrored by ci.yml.
gate-full: fmt vet build test cover lint gosec gitleaks pii smoke
	@echo "gate-full: OK"

tools:
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GO) install github.com/securego/gosec/v2/cmd/gosec@latest
	$(GO) install golang.org/x/vuln/cmd/govulncheck@latest

clean:
	rm -f $(BINARY) cover.out
