# Source-only project: users build locally. No gcc.
#   make          → eggbot
#   make test     → go test ./...
#   make run      → build + ./eggbot -c eggbot.toml  (copy the example first)

CGO_ENABLED ?= 0
GO          ?= go
BIN         ?= eggbot
CONFIG      ?= eggbot.toml
# Version string comes from internal/version/version.go. Do not hardcode it here.
VERSION     ?= $(shell awk -F'"' '/^[[:space:]]*Version = / { print $$2; exit }' internal/version/version.go)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     ?= -X eggbot/internal/version.Version=$(VERSION) -X eggbot/internal/version.Commit=$(COMMIT) -X eggbot/internal/version.Date=$(DATE)

export CGO_ENABLED

.PHONY: all build test test-race coverage vet staticcheck vulncheck check fuzz run clean sbom

all: build

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/eggbot

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

coverage:
	$(GO) test -count=1 -covermode=set -coverpkg=./... -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out
	OVERALL_MIN=85 CRITICAL_MIN=90 sh scripts/check-coverage.sh coverage.out

fuzz:
	$(GO) test -fuzz=FuzzISupport -fuzztime=10s ./internal/ircx
	$(GO) test -fuzz=FuzzEncodeLine -fuzztime=10s ./internal/ircx

vet:
	$(GO) vet ./...

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...

vulncheck:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

sbom: build
	$(GO) version -m $(BIN) > eggbot.sbom.txt

check: test test-race vet staticcheck vulncheck

run: build
	./$(BIN) -c $(CONFIG)

clean:
	rm -f $(BIN) $(BIN).prev coverage.out eggbot.sbom.txt SHA256SUMS SHA256SUMS.asc
