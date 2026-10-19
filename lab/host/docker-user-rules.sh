#!/usr/bin/env bash
# Docker sets the FORWARD policy to DROP, which silently drops traffic on Incus bridges.
# Accept forwarded traffic on Incus/lab bridges via Docker's DOCKER-USER hook chain.
#   docker-user-rules.sh add|del
set -euo pipefail

# incusbr0 (default bridge) and of-* (lab networks created from topology.yml)
BRIDGES=(incusbr+ of-+)

action="${1:?usage: $0 add|del}"

if ! iptables -n -L DOCKER-USER >/dev/null 2>&1; then
	echo "DOCKER-USER chain not present (is Docker running?); nothing to do"
	exit 0
fi

for br in "${BRIDGES[@]}"; do
	for dir in -i -o; do
		rule=(DOCKER-USER "$dir" "$br" -j ACCEPT)
		case $action in
		add) iptables -C "${rule[@]}" 2>/dev/null || iptables -I "${rule[@]}" ;;
		del) iptables -D "${rule[@]}" 2>/dev/null || true ;;
		*)
			echo "unknown action: $action" >&2
			exit 2
			;;
		esac
	done
done
