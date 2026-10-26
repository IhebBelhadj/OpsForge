SHELL_SCRIPTS := $(shell find . -name '*.sh' -not -path './.git/*') ansible/roles/dns/files/opsforge-geo
# Fall back to the official image when the local binary is missing or broken (e.g. Arch partial upgrade)
SHELLCHECK := $(shell shellcheck --version >/dev/null 2>&1 && echo shellcheck || \
	echo docker run --rm -v "$(CURDIR):/mnt:ro" -w /mnt koalaman/shellcheck:v0.11.0)

.PHONY: check test lint-shell lint-yaml lint-python lint-ansible validate preflight smoke up down provision verify host-setup

# OpsForge needs Python only: OpsForge Academy (its own repository) is checked by its own Makefile.
check: lint-shell lint-yaml lint-python lint-ansible validate test

test:
	python3 -m pytest -q tests

lint-shell:
	$(SHELLCHECK) $(SHELL_SCRIPTS)

lint-yaml:
	yamllint .

lint-python:
	python3 -m py_compile opsforge lab/render.py lab/scenario.py lab/contract.py lab/apiserver.py

lint-ansible:
	@if [ -d ansible ]; then ansible-lint ansible; else echo "lint-ansible: no ansible/ yet, skipping"; fi

validate:
	./opsforge validate

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
