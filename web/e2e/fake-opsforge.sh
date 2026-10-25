#!/usr/bin/env bash
# The lab toolchain as the e2e suite sees it: answers `opsforge api <command> --json` with contract 1 and an empty
# lab, and finds every scenario step valid (academy compiles the demo pack, academy/testdata/packs: one course with
# every item type), without Incus or Python (docs/courses/testing.md, "End to end").
set -euo pipefail

case "${2:-}" in
	version) echo '{"contract": 1, "minor": 0, "opsforge": "1.0.0-fake"}' ;;
	state) echo '{"contract": 1, "up": false, "error": null, "topology": "topology.yml", "layout": "stretched", "sites": {}, "links": {}, "nodes": [], "fault": false, "placeholders": {}}' ;;
	validate-steps) cat >/dev/null && echo '{"contract": 1, "errors": []}' ;;
	*)
		echo '{"contract": 1, "type": "/problems/unknown-command", "title": "Unknown command", "detail": "fake toolchain"}' >&2
		exit 1
		;;
esac
