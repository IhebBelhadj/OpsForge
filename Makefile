SHELL_SCRIPTS := $(shell find . -name '*.sh' -not -path './.git/*' -not -path './web/node_modules/*') ansible/roles/dns/files/opsforge-geo
# Fall back to the official image when the local binary is missing or broken (e.g. Arch partial upgrade)
SHELLCHECK := $(shell shellcheck --version >/dev/null 2>&1 && echo shellcheck || \
	echo docker run --rm -v "$(CURDIR):/mnt:ro" -w /mnt koalaman/shellcheck:v0.11.0)

.PHONY: check test lint-shell lint-yaml lint-python lint-ansible validate preflight smoke up down provision verify host-setup \
	academy academy-test academy-lint web-deps web-build web-check gen gen-check e2e

check: lint-shell lint-yaml lint-python lint-ansible validate test academy-lint academy-test web-check gen-check

test:
	python3 -m pytest -q tests

lint-shell:
	$(SHELLCHECK) $(SHELL_SCRIPTS)

lint-yaml:
	yamllint .

lint-python:
	python3 -m py_compile opsforge lab/render.py lab/scenario.py lab/labs.py lab/labsdb.py lab/labserver.py lab/academy.py

lint-ansible:
	@if [ -d ansible ]; then ansible-lint ansible; else echo "lint-ansible: no ansible/ yet, skipping"; fi

validate:
	./opsforge validate
	./opsforge labs validate
	./opsforge academy compile

preflight:
	./opsforge preflight

smoke:
	./opsforge smoke

up:
	./opsforge up

down:
	./opsforge down

provision:
	./opsforge provision

verify:
	./opsforge verify

host-setup:
	@echo "Run as root: sudo ./scripts/host-setup.sh"

# Academy (Phase 11, docs/courses/): the Go platform in academy/ with the React app from web/ embedded.
# Go tools (sqlc, oapi-codegen, wire, golangci-lint) are pinned in academy/tools/go.mod.
ACADEMY_VERSION := $(shell git describe --always --dirty 2>/dev/null || echo dev)
GO_TOOL := go tool -modfile=tools/go.mod

academy: web-build
	rm -rf academy/web/dist && mkdir -p academy/web/dist && cp -r web/dist/. academy/web/dist/
	touch academy/web/dist/.gitkeep
	cd academy && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(ACADEMY_VERSION)" \
		-o bin/academy ./cmd/academy
	@echo "built academy/bin/academy $(ACADEMY_VERSION): ./academy/bin/academy serve"

academy-test:
	cd academy && go test -race ./...

academy-lint:
	cd academy && go vet ./... && $(GO_TOOL) golangci-lint run ./...

web-deps:
	cd web && pnpm install --frozen-lockfile

web-build: web-deps
	cd web && pnpm build

web-check: web-deps
	cd web && pnpm typecheck && pnpm lint && pnpm format:check && pnpm test

# Generated code: Go queries (sqlc), the HTTP server (oapi-codegen), the object graph (wire), the web client and
# route tree. Generated files are committed; gen-check fails when they are stale.
gen: web-deps
	cd academy && $(GO_TOOL) sqlc generate
	cd academy && $(GO_TOOL) oapi-codegen -config api/oapi-codegen.yaml api/openapi.yaml
	cd academy && $(GO_TOOL) wire gen ./cmd/academy >/dev/null
	cd web && pnpm gen >/dev/null

GENERATED_HASHES = find academy web/src -type f -not -path '*/node_modules/*' | sort | xargs sha256sum

gen-check:
	@$(GENERATED_HASHES) > .gen-before
	@$(MAKE) --no-print-directory gen >/dev/null 2>&1
	@$(GENERATED_HASHES) > .gen-after
	@diff .gen-before .gen-after >/dev/null && rm -f .gen-before .gen-after || { \
		diff .gen-before .gen-after; rm -f .gen-before .gen-after; \
		echo "generated code was stale: make gen rewrote the files above (review and commit them)"; exit 1; }

# End to end: the built binary with a fake lab toolchain, driven by Playwright (web/e2e/)
e2e: academy
	cd web && pnpm e2e
