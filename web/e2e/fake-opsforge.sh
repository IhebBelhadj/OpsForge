#!/usr/bin/env bash
# The lab toolchain as the e2e suite sees it: answers `opsforge academy <command> --json` with contract 1, an
# empty lab and the golden demo pack (one course with every item type), without Incus or Python
# (docs/courses/testing.md, "End to end").
set -euo pipefail

testdata="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../academy/testdata" && pwd)"

case "${2:-}" in
	version) echo '{"contract": 1}' ;;
	state) echo '{"contract": 1, "up": false, "error": null, "topology": "topology.yml", "layout": "stretched", "sites": {}, "links": {}, "nodes": [], "fault": false}' ;;
	compile) cat "$testdata/bundle.golden.json" ;;
	*)
		echo '{"contract": 1, "type": "/problems/unknown-command", "title": "Unknown command", "detail": "fake toolchain"}' >&2
		exit 1
		;;
esac
