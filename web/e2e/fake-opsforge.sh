#!/usr/bin/env bash
# The lab toolchain as the e2e suite sees it: answers `opsforge academy <command> --json` with contract 1 and an
# empty lab, without Incus or Python (docs/courses/testing.md, "End to end").
set -euo pipefail

case "${2:-}" in
	version) echo '{"contract": 1}' ;;
	state) echo '{"contract": 1, "up": false, "error": null, "topology": "topology.yml", "layout": "stretched", "sites": {}, "links": {}, "nodes": [], "fault": false}' ;;
	compile) echo '{"contract": 1, "compiled_at": "2026-10-24T00:00:00Z", "packs": [], "errors": []}' ;;
	*)
		echo '{"contract": 1, "type": "/problems/unknown-command", "title": "Unknown command", "detail": "fake toolchain"}' >&2
		exit 1
		;;
esac
