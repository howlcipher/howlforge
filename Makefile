# HowlForge build and verification targets.
#
# Everything here uses the Go toolchain and nothing else. A contributor with Go
# installed can run every target with no further setup, which is deliberate: a
# tool whose job is to keep working when other things break should not itself
# need exotic tooling to build.

BINARY := build/howlforge
PACKAGES := ./...

.PHONY: all build test test-race vet fmt fmt-check lint validate verify clean install help

all: verify

## build: compile the CLI into build/howlforge
build:
	go build -o $(BINARY) ./cmd/howlforge

## test: run the full test suite
test:
	go test $(PACKAGES)

## test-race: run the full test suite under the race detector
test-race:
	go test -race $(PACKAGES)

## test-verbose: run the full test suite with per test output
test-verbose:
	go test -v $(PACKAGES)

## cover: report statement coverage per package
cover:
	go test -coverprofile=coverage.out $(PACKAGES)
	go tool cover -func=coverage.out

## vet: run the standard Go static checks
vet:
	go vet $(PACKAGES)

## fmt: format every Go file in place
fmt:
	gofmt -w .

## fmt-check: fail if any Go file is not formatted
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "these files are not formatted:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

## validate: check the shipped example configuration
validate: build
	$(BINARY) --config config validate

## doctor: report the effective environment
doctor: build
	$(BINARY) --config config doctor

## verify: the full gate to run before pushing
verify: fmt-check vet test validate
	@echo "verification passed"

## install: install the CLI into GOBIN
install:
	go install ./cmd/howlforge

## clean: remove build and coverage artifacts
clean:
	rm -rf build coverage.out coverage.html

## help: list the available targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | sort
