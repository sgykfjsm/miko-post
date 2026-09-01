# miko-post build targets.
#
# VERSION and COMMIT are stamped into internal/version at link time. Builds that
# cannot pass linker flags — notably `go install` — fall back to the build info
# the toolchain records; see internal/version/version.go.

BINARY      := mp
CMD         := ./cmd/mp
BIN_DIR     := ./bin
VERSION_PKG := github.com/sgykfjsm/miko-post/internal/version

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

LDFLAGS := -X $(VERSION_PKG).version=$(VERSION) -X $(VERSION_PKG).commit=$(COMMIT)

.PHONY: all build test race vet fmt check install clean

all: check build

## build: compile the binary into ./bin with version stamping
build:
	@mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD)

## test: run the test suite
test:
	go test ./...

## race: run the test suite under the race detector
##
## Not optional for internal/post: sink independence is a concurrency guarantee,
## and these tests are the only place it is mechanically checked.
race:
	go test -race ./...

## vet: run go vet
vet:
	go vet ./...

## fmt: report files that gofmt would change
fmt:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed:"; echo "$$unformatted"; exit 1; \
	fi

## check: fmt, vet, and the race-enabled test suite
check: fmt vet race

## install: install mp onto the GOPATH bin directory
install:
	go install -ldflags "$(LDFLAGS)" $(CMD)

## clean: remove build output
clean:
	rm -rf $(BIN_DIR)
