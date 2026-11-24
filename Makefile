BINARY    ?= kaclaig
BIN       := bin/$(BINARY)
PKG       := ./...
COV       ?= coverage.out
GOFLAGS   ?=
PREFIX    ?= /usr/local
MANPREFIX ?= $(PREFIX)/share/man
MAN1DIR   ?= $(MANPREFIX)/man1
DESTDIR   ?=
CHANGELOG ?= CHANGELOG.md

# Release controls:
#   KACLIAIG=0      skip kaclaig entirely (bare version-header bump, no semver
#                   check); enabled by default
#   SEMVER_CHECK=0  keep kaclaig but skip its semver check; enabled by default
#   KACLIAIG_FLAGS  extra flags passed to kaclaig (e.g. -C '<json>')
KACLIAIG       ?= 1
SEMVER_CHECK   ?= 1
KACLIAIG_FLAGS ?=

.PHONY: all build install test test-race coverage vet fmt fmt-check clean \
        check-clean version-bump-major version-bump-minor version-bump-patch tag

all: fmt-check vet build

## build: compile the CLI into bin/kaclaig
build:
	go build $(GOFLAGS) -o $(BIN) ./cmd/kaclaig

## install: install the CLI into GOBIN and the man page into $(MAN1DIR)
install:
	go install $(GOFLAGS) ./cmd/kaclaig
	install -d "$(DESTDIR)$(MAN1DIR)"
	install -m 0644 man/kaclaig.1 "$(DESTDIR)$(MAN1DIR)/$(BINARY).1"

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

## check-clean: fail if the working tree has tracked or untracked changes
check-clean:
	@if [ -n "$$(git status --porcelain 2>/dev/null)" ]; then \
		echo "error: working tree is not clean; commit or stash all changes first" >&2; \
		git status --short >&2; \
		exit 1; \
	fi

# Shared implementation for the version-bump-* targets (not a public target).
define VERSION_BUMP
	@case "$(1)" in \
		major) bump_flag=-M ;; \
		minor) bump_flag=-m ;; \
		patch) bump_flag=-p ;; \
	esac; \
	if [ "$(KACLIAIG)" = 0 ]; then \
		echo "kaclaig disabled: writing a bare version header (no changelog generation or semver check)" >&2; \
		raw=$$(git describe --tags --abbrev=0 2>/dev/null || true); \
		if [ -z "$$raw" ]; then \
			new=0.1.0; \
		else \
			core=$${raw#v}; core=$${core#V}; \
			suffix=""; \
			case "$$core" in *-*) suffix="-$${core#*-}"; core=$${core%%-*} ;; esac; \
			meta=""; \
			case "$$core" in *+*) meta="+$${core#*+}"; core=$${core%%+*} ;; esac; \
			major=$${core%%.*}; rest=$${core#*.}; \
			if [ "$$rest" = "$$core" ]; then rest=0.0; fi; \
			minor=$${rest%%.*}; patch=$${rest#*.}; \
			if [ "$$patch" = "$$rest" ]; then patch=0; fi; \
			case "$(1)" in \
				major) new="$$((major + 1)).0.0$$suffix$$meta" ;; \
				minor) new="$$major.$$((minor + 1)).0$$suffix$$meta" ;; \
				patch) new="$$major.$$minor.$$((patch + 1))$$suffix$$meta" ;; \
			esac; \
		fi; \
		tmp=$$(mktemp "$(CHANGELOG).XXXXXX") || exit 1; \
		{ printf '## [%s] - %s\n\n' "$$new" "$$(date +%F)"; \
		  if [ -f "$(CHANGELOG)" ]; then cat "$(CHANGELOG)"; fi; \
		} > "$$tmp" && mv "$$tmp" "$(CHANGELOG)"; \
		echo "bumped to $$new in $(CHANGELOG) (uncommitted)"; \
	else \
		args="$$bump_flag -w -o $(CHANGELOG)"; \
		if [ "$(SEMVER_CHECK)" != 0 ]; then args="$$args -S"; fi; \
		$(BIN) $$args $(KACLIAIG_FLAGS); \
		echo "version bump complete; changes are left uncommitted (commit them, then run make tag)"; \
	fi
endef

## version-bump-minor: bump the minor version and write the changelog entry; never commits
version-bump-minor: build
	$(call VERSION_BUMP,minor)

## version-bump-major: bump the major version and write the changelog entry; never commits
version-bump-major: build
	$(call VERSION_BUMP,major)

## version-bump-patch: bump the patch version and write the changelog entry; never commits
version-bump-patch: build
	$(call VERSION_BUMP,patch)

## tag: tag the CHANGELOG version after a clean-tree check and kaclaig validation
tag: check-clean build
	@version=$$(sed -nE 's/^## \[([^]]+)\].*/\1/p' "$(CHANGELOG)" | grep -vx 'Unreleased' | head -n 1); \
	if [ -z "$$version" ]; then \
		echo "error: no released version found in $(CHANGELOG) (expected '## [x.y.z]')" >&2; exit 1; \
	fi; \
	for candidate in "$$version" "v$$version" "V$$version"; do \
		if git rev-parse -q --verify "refs/tags/$$candidate" >/dev/null 2>&1; then \
			echo "error: tag '$$candidate' already exists" >&2; exit 1; \
		fi; \
	done; \
	if [ "$(KACLIAIG)" = 0 ]; then \
		echo "kaclaig disabled: skipping changelog/semver validation" >&2; \
	else \
		args="-t $$version -o $(CHANGELOG)"; \
		if [ "$(SEMVER_CHECK)" != 0 ]; then args="$$args -S"; fi; \
		prev=$$(git describe --tags --abbrev=0 2>/dev/null || true); \
		if [ -n "$$prev" ]; then args="$$args -f $$prev"; fi; \
		echo "validating $$version with kaclaig..." >&2; \
		$(BIN) $$args $(KACLIAIG_FLAGS) || exit 1; \
	fi; \
	git tag -a "$$version" -m "release $$version" && echo "tagged $$version"
