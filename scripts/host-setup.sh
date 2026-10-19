#!/usr/bin/env bash
# One-time host preparation for the OpsForge lab (Arch Linux). Idempotent, safe to re-run.
#   sudo ./scripts/host-setup.sh
# Env: OPSFORGE_POOL_SIZE  max size of the sparse btrfs storage pool (default 25GiB)
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HOST_DIR="$REPO_DIR/lab/host"
POOL_SIZE="${OPSFORGE_POOL_SIZE:-25GiB}"
TARGET_USER="${SUDO_USER:-}"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*" >&2; }
die() {
	printf '\033[1;31mxx\033[0m %s\n' "$*" >&2
	exit 1
}

[[ $EUID -eq 0 ]] || die "run as root: sudo $0"
command -v pacman >/dev/null || die "this script targets Arch Linux (pacman not found)"

if command -v checkupdates >/dev/null && [[ -n $(checkupdates 2>/dev/null) ]]; then
	warn "pending system updates: installing on an outdated system can break packages (partial upgrade); run 'pacman -Syu' first"
fi

log "Installing packages"
pacman -S --needed --noconfirm \
	incus btrfs-progs \
	shellcheck ansible ansible-lint yamllint python-yaml python-jsonschema

log "Mapping subordinate ids for unprivileged containers"
for f in /etc/subuid /etc/subgid; do
	grep -q '^root:' "$f" 2>/dev/null || echo 'root:1000000:1000000000' >>"$f"
done

log "Loading kernel modules (persisted in /etc/modules-load.d/opsforge.conf)"
install -Dm644 "$HOST_DIR/modules-load.conf" /etc/modules-load.d/opsforge.conf
grep -Ev '^\s*(#|$)' "$HOST_DIR/modules-load.conf" | xargs -n1 modprobe

log "Applying sysctls (persisted in /etc/sysctl.d/90-opsforge.conf)"
install -Dm644 "$HOST_DIR/sysctl.conf" /etc/sysctl.d/90-opsforge.conf
sysctl -q --load /etc/sysctl.d/90-opsforge.conf

if systemctl cat docker.service >/dev/null 2>&1; then
	log "Docker found: installing DOCKER-USER rules so Incus bridges are not dropped"
	install -Dm755 "$HOST_DIR/docker-user-rules.sh" /usr/local/libexec/opsforge/docker-user-rules.sh
	install -Dm644 "$HOST_DIR/opsforge-docker-user.service" /etc/systemd/system/opsforge-docker-user.service
	systemctl daemon-reload
	systemctl enable opsforge-docker-user.service
	if systemctl is-active -q docker.service; then
		systemctl restart opsforge-docker-user.service
	fi
fi

log "Starting Incus"
systemctl enable --now incus.socket incus.service
incus admin waitready --timeout=60

if incus storage show default >/dev/null 2>&1; then
	log "Incus already initialised, skipping preseed"
else
	free_gib=$(($(df --output=avail -B1G /var/lib | tail -1)))
	if ((free_gib < 35)); then
		warn "only ${free_gib} GiB free on /var/lib; pool may fill the disk (OPSFORGE_POOL_SIZE=${POOL_SIZE})"
	fi
	log "Initialising Incus (btrfs pool ${POOL_SIZE}, bridge incusbr0 10.99.0.1/24)"
	sed "s/@POOL_SIZE@/${POOL_SIZE}/" "$HOST_DIR/incus-preseed.yaml" | incus admin init --preseed
fi

if [[ -n $TARGET_USER ]] && ! id -nG "$TARGET_USER" | grep -qw incus-admin; then
	log "Adding $TARGET_USER to incus-admin (log out/in or run 'newgrp incus-admin' to apply)"
	usermod -aG incus-admin "$TARGET_USER"
fi

log "Done. Next: ./opsforge preflight && ./opsforge smoke"
