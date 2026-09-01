# miko-post build targets.
#
# VERSION and COMMIT are stamped into internal/version at link time. They can be
# overridden from the environment or the command line; otherwise they are derived
# from git. Builds that are handed no linker flags — notably a plain `go install`
# by an end user — fall back to the build info the toolchain records; see
# internal/version/version.go.

# `override` throughout: every one of these is interpolated by make directly
# into a recipe's shell words, so under MAKEFLAGS=-e an ambient value would be
# executed. None is a documented knob — the command is mp, its source is
# ./cmd/mp, and output goes to ./bin — so locking them costs nothing and also
# keeps `clean`'s recursive delete pointed at the build directory.
override BINARY  := mp
override CMD     := ./cmd/mp
override BIN_DIR := ./bin
# Interpolated into shell words like the above; VERSION and COMMIT are the only
# intended knobs, and they are read as inert shell data rather than interpolated.
override VERSION_PKG := github.com/sgykfjsm/miko-post/internal/version

# Exported so the recipe shell reads these directly rather than having make
# interpolate them into a command string. See STAMP below.
export VERSION
export COMMIT

# STAMP resolves $version and $commit and assembles $ldflags for a recipe.
#
# Several rules make this more careful than it first appears.
#
# Nothing untrusted is interpolated by make. A git tag name may legally contain
# `$`, backticks, and quotes (git only forbids space, `~^:?*[\` and control
# characters), and make does not rescan $(shell ...) output before handing a
# recipe to the shell. Interpolating a tag would therefore let a tag name execute
# during `make build`. Reading git's output into a shell variable keeps it inert:
# the shell does not re-expand a variable's value.
#
# git's inherited environment is cleared before the stamp is resolved. A hook or
# `git submodule foreach` exports GIT_DIR, GIT_WORK_TREE and GIT_INDEX_FILE
# (sometimes relative), which would make the stamp describe a different
# repository than the one being compiled. GIT_COMMON_DIR and the object-directory
# variables do the same, and GIT_CONFIG_GLOBAL / GIT_CONFIG_COUNT can inject
# core.fsmonitor, which `git status` executes.
#
# Dirtiness comes from `git status --porcelain --untracked-files=normal`, which
# reports untracked files and overrides a user's status.showUntrackedFiles=no.
# `git describe --dirty` sees neither, and Go compiles every .go file in a
# package, so an untracked source file would otherwise be stamped as a clean
# release — while the toolchain's own build info correctly reports it as modified.
#
# A git-derived value is filtered to a conservative character set, and the filter
# reports when it changed something rather than rewriting silently. An explicitly
# supplied VERSION or COMMIT is never rewritten: it is validated and the build
# fails, because silently shipping a release labelled differently from what the
# operator asked for is worse than stopping.
#
# One caveat on that guarantee: make expands $(...) in a variable given on its
# own command line before the recipe ever runs, so `make VERSION='1.0$(x)'`
# arrives as `1.0` and cannot be rejected here. The environment form is checked.
#
# LC_ALL=C makes the validation byte-based. Under a UTF-8 locale a shell bracket
# range is collation-ordered, so [!A-Za-z0-9._+/-] does not match an accented
# letter and the guard would silently pass a value it is meant to reject.
override STAMP_CHARS := A-Za-z0-9._+/-

define STAMP
set -eu; \
LC_ALL=C; export LC_ALL; \
version=$${VERSION:-}; commit=$${COMMIT:-}; \
version_explicit=false; commit_explicit=false; \
[ -z "$$version" ] || version_explicit=true; \
[ -z "$$commit" ] || commit_explicit=true; \
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR \
  GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES \
  GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM GIT_CONFIG_COUNT GIT_CONFIG_PARAMETERS; \
if [ "$$version_explicit" = false ]; then \
  version=$$(git describe --tags --always 2>/dev/null || echo dev); \
  if [ -n "$$(git status --porcelain --untracked-files=normal 2>/dev/null)" ]; then \
    version="$$version+dirty"; \
  fi; \
  filtered=$$(printf '%s' "$$version" | tr -cd '$(STAMP_CHARS)'); \
  if [ "$$filtered" != "$$version" ]; then \
    printf 'warning: version stamp filtered from %s to %s\n' "$$version" "$$filtered" >&2; \
  fi; \
  version=$$filtered; \
  [ -n "$$version" ] || version=dev; \
fi; \
if [ "$$commit_explicit" = false ]; then \
  commit=$$(git rev-parse --short HEAD 2>/dev/null || echo unknown); \
  filtered=$$(printf '%s' "$$commit" | tr -cd '$(STAMP_CHARS)'); \
  if [ "$$filtered" != "$$commit" ]; then \
    printf 'warning: commit stamp filtered from %s to %s\n' "$$commit" "$$filtered" >&2; \
  fi; \
  commit=$$filtered; \
  [ -n "$$commit" ] || commit=unknown; \
fi; \
case "$$version" in *[!$(STAMP_CHARS)]*) \
  printf 'error: VERSION (from the environment or command line) must match [%s]+, got: %s\n' \
    '$(STAMP_CHARS)' "$$version" >&2; exit 1;; esac; \
case "$$commit" in *[!$(STAMP_CHARS)]*) \
  printf 'error: COMMIT (from the environment or command line) must match [%s]+, got: %s\n' \
    '$(STAMP_CHARS)' "$$commit" >&2; exit 1;; esac; \
ldflags="-X $(VERSION_PKG).version=$$version -X $(VERSION_PKG).commit=$$commit"
endef

.PHONY: all build test race vet fmt check install clean stamp

all: check build

## build: compile the binary into ./bin with version stamping
build:
	@mkdir -p $(BIN_DIR)
	@$(STAMP); \
	echo "go build -ldflags \"$$ldflags\" -o $(BIN_DIR)/$(BINARY) $(CMD)"; \
	go build -ldflags "$$ldflags" -o $(BIN_DIR)/$(BINARY) $(CMD)

## stamp: print the version and commit that a build would embed
stamp:
	@$(STAMP); \
	printf 'version=%s\ncommit=%s\n' "$$version" "$$commit"

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

## fmt: fail if gofmt reports changes, or cannot parse the tree
##
## gofmt exits non-zero on a file it cannot parse while printing nothing to
## stdout, so the exit status has to be checked as well as the file list.
fmt:
	@set -eu; \
	if ! unformatted=$$(gofmt -l . 2>&1); then \
		echo "gofmt failed:"; echo "$$unformatted"; exit 1; \
	fi; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed:"; echo "$$unformatted"; exit 1; \
	fi

## check: fmt, vet, and the race-enabled test suite
check: fmt vet race

## install: install mp onto the GOPATH bin directory
install:
	@$(STAMP); \
	echo "go install -ldflags \"$$ldflags\" $(CMD)"; \
	go install -ldflags "$$ldflags" $(CMD)

## clean: remove build output
clean:
	rm -rf -- "$(BIN_DIR)"
