#!/usr/bin/env bash
# Runs a command against a throwaway `opsforge api serve` (its own data folder, a free loopback port), with
# ACADEMY_OPSFORGE_URL and ACADEMY_OPSFORGE_TOKEN_FILE set for it: `make check` compiles the packs and runs Academy's
# integration tests this way. Usage: scripts/with-opsforge-api.sh <command> [args...]
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
data="$(mktemp -d)"
port="$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')"
"$root/opsforge" api serve --port "$port" --data "$data" >"$data/serve.log" 2>&1 &
server=$!
cleanup() {
	kill "$server" 2>/dev/null || true
	wait "$server" 2>/dev/null || true
	rm -rf "$data"
}
trap cleanup EXIT

for _ in $(seq 50); do
	if [ -s "$data/api-token" ] && python3 -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:$port/v1/version')" 2>/dev/null; then
		break
	fi
	sleep 0.1
done
export ACADEMY_OPSFORGE_URL="http://127.0.0.1:$port" ACADEMY_OPSFORGE_TOKEN_FILE="$data/api-token" OPSFORGE_API_DATA="$data"
"$@"
