# Targets from plan/TESTING.md §1.
GO ?= go

.PHONY: test check fmt-check vet lint web-lint e2e e2e-update fuzz-short fuzz-long fuzz bench golden-update crash crash-long

test:
	$(GO) test -race -count=1 ./...

check: fmt-check vet lint test fuzz-short web-lint

fmt-check:
	@files="$$(git ls-files -co --exclude-standard '*.go')"; \
	out="$$([ -z "$$files" ] || gofmt -l $$files)"; \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

vet:
	$(GO) vet ./...
	$(GO) vet -tags dev ./...

lint:
	golangci-lint run
	golangci-lint run --build-tags dev

# The web asset checks (plan/TESTING.md §6); no Node needed.
web-lint:
	$(GO) test -count=1 -run '^TestWebAssets_' ./internal/web

# Playwright on Chromium, Firefox and WebKit (plan/TESTING.md §5). Needs Node
# from mise.toml and the browsers: `cd e2e && npx playwright install`.
# E2E_ARGS passes options through, e.g. E2E_ARGS="--project=chromium".
e2e:
	cd e2e && npm ci --no-audit --no-fund --loglevel=error && npx playwright test $(E2E_ARGS)

e2e-update:
	@$(MAKE) --no-print-directory e2e E2E_ARGS="--update-snapshots $(E2E_ARGS)"

fuzz-short:
	@$(MAKE) --no-print-directory fuzz FUZZTIME=10s

fuzz-long:
	@$(MAKE) --no-print-directory fuzz FUZZTIME=10m

# Runs every Fuzz* target once; go test -fuzz accepts one target per run.
fuzz:
	@set -e; for pkg in $$($(GO) list ./...); do \
		for f in $$($(GO) test -list '^Fuzz' $$pkg | grep '^Fuzz' || true); do \
			echo "fuzz $$pkg $$f ($(FUZZTIME))"; \
			$(GO) test -run='^$$' -fuzz="^$$f$$" -fuzztime=$(FUZZTIME) $$pkg; \
		done; \
	done

bench:
	$(GO) test -run='^$$' -bench=. -benchmem ./...

# Only packages with golden files define -update; other test binaries would
# reject the flag.
golden-update:
	@pkgs="$$(git ls-files -co --exclude-standard '*/testdata/golden/*' | sed 's|/testdata/golden/.*||' | sort -u | sed 's|^|./|')"; \
	if [ -z "$$pkgs" ]; then echo "no golden files"; else $(GO) test $$pkgs -update; fi

crash:
	$(GO) test -tags crash -count=1 -run '^TestCrash' ./...

# Nightly: 2,000 kills of the typing child (plan/TESTING.md §7).
crash-long:
	CRASH_ITERATIONS=2000 $(GO) test -tags crash -count=1 -timeout 60m -run '^TestCrash_Typing$$' ./...
