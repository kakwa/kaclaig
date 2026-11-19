BINARY  ?= kaclaig
BIN     := bin/$(BINARY)
PKG     := ./...
COV     ?= coverage.out
GOFLAGS ?=

.PHONY: all build install test test-race coverage vet fmt fmt-check clean

all: fmt-check vet build

## build: compile the CLI into bin/kaclaig
build:
	go build $(GOFLAGS) -o $(BIN) ./cmd/kaclaig

## install: install the CLI into GOBIN
install:
	go install $(GOFLAGS) ./cmd/kaclaig

## test: run all tests
test:
	go test $(GOFLAGS) -count=1 $(PKG)

## test-race: run all tests with the race detector
test-race:
	go test $(GOFLAGS) -count=1 -race $(PKG)

## coverage: run tests with coverage, write $(COV), and print a summary
coverage:
	go test $(GOFLAGS) -count=1 -coverprofile=$(COV) -covermode=atomic $(PKG)
	@echo "--- coverage summary ---"
	go tool cover -func=$(COV) | tail -n 1
	@echo "--- per-file ---"
	go tool cover -func=$(COV) | awk '$$3 == "0.0%" {print "  uncovered: " $$1 " " $$2}'

## vet: run go vet
vet:
	go vet $(PKG)

## fmt: format all Go files
fmt:
	gofmt -l -w .

## fmt-check: fail if any Go files are not gofmt-clean
fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed for:"; echo "$$out"; exit 1; fi

## clean: remove build artifacts and coverage output
clean:
	rm -rf bin $(COV)
