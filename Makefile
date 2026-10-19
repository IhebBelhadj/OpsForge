SHELL_SCRIPTS := $(shell find . -name '*.sh' -not -path './.git/*')
# Fall back to the official image when the local binary is missing or broken (e.g. Arch partial upgrade)
SHELLCHECK := $(shell shellcheck --version >/dev/null 2>&1 && echo shellcheck || \
	echo docker run --rm -v "$(CURDIR):/mnt:ro" -w /mnt koalaman/shellcheck:v0.11.0)

.PHONY: check lint-shell lint-yaml lint-python lint-ansible validate preflight smoke host-setup

check: lint-shell lint-yaml lint-python lint-ansible validate

lint-shell:
	$(SHELLCHECK) $(SHELL_SCRIPTS)

lint-yaml:
	yamllint .

lint-python:
	python3 -m py_compile opsforge

# ansible/ arrives in Phase 2
lint-ansible:
	@if [ -d ansible ]; then ansible-lint ansible; else echo "lint-ansible: no ansible/ yet, skipping"; fi

validate:
	./opsforge validate

preflight:
	./opsforge preflight

smoke:
	./opsforge smoke

host-setup:
	@echo "Run as root: sudo ./scripts/host-setup.sh"
