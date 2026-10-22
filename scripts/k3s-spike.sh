#!/usr/bin/env bash
# Phase 2.5 spike: can K3s run in an *unprivileged* Incus container? Runs outside the lab (on incusbr0) and
# removes itself afterwards. Prints a verdict and the memory K3s costs. Usage: scripts/k3s-spike.sh [keep]
set -euo pipefail

NAME=opsforge-k3s-spike
PROFILE=opsforge-k3s-spike  # not k3s-node: the lab uses that name (opsforge up)
IMAGE=images:debian/13
KEEP=${1:-}

cleanup() {
	if [ "$KEEP" != keep ]; then
		incus delete --force "$NAME" >/dev/null 2>&1 || true
		incus profile delete "$PROFILE" >/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT

incus profile show "$PROFILE" >/dev/null 2>&1 || incus profile create "$PROFILE"
# Nesting for containerd; /dev/kmsg for kubelet; modules are loaded by Incus on the host (not by the container).
incus profile set "$PROFILE" \
	security.nesting=true \
	security.privileged=false \
	linux.kernel_modules=overlay,br_netfilter,nf_conntrack,ip_tables,iptable_nat,xt_conntrack \
	limits.memory=1536MiB limits.cpu=2
incus profile device add "$PROFILE" kmsg unix-char source=/dev/kmsg path=/dev/kmsg 2>/dev/null || true

incus delete --force "$NAME" >/dev/null 2>&1 || true
incus launch "$IMAGE" "$NAME" -p default -p "$PROFILE"
for _ in $(seq 60); do
	incus exec "$NAME" -- getent hosts deb.debian.org >/dev/null 2>&1 && break
	sleep 1
done

incus exec "$NAME" -- bash -euc '
	apt-get update -qq && apt-get install -y -qq curl ca-certificates >/dev/null
	# KubeletInUserNamespace lets kubelet tolerate the read-only sysctls of an unprivileged container.
	curl -sfL https://get.k3s.io | INSTALL_K3S_SKIP_START=true sh - >/dev/null
	mkdir -p /etc/rancher/k3s
	cat >/etc/rancher/k3s/config.yaml <<CFG
disable: [traefik, servicelb, metrics-server]
kubelet-arg: ["feature-gates=KubeletInUserNamespace=true"]
CFG
	systemctl start k3s
'

echo "waiting for the node to become Ready (up to 3 min)"
ready=no
for _ in $(seq 90); do
	if incus exec "$NAME" -- k3s kubectl get nodes 2>/dev/null | grep -qw Ready; then
		ready=yes
		break
	fi
	sleep 2
done

if [ "$ready" = yes ]; then
	incus exec "$NAME" -- k3s kubectl run probe --image=rancher/mirrored-pause:3.6 --restart=Never >/dev/null 2>&1 || true
	incus exec "$NAME" -- k3s kubectl wait --for=condition=Ready pod --all -A --timeout=180s >/dev/null
	echo "== pods"
	incus exec "$NAME" -- k3s kubectl get pods -A
	echo "== memory (anon + kernel, MiB)"
	# shellcheck disable=SC2016 # the awk program runs inside the container
	incus exec "$NAME" -- awk '/^(anon|kernel|file) /{printf "%s %d\n", $1, $2/1048576}' /sys/fs/cgroup/memory.stat
	echo "VERDICT: K3s runs in an unprivileged container"
else
	echo "== k3s log tail"
	incus exec "$NAME" -- journalctl -u k3s --no-pager -n 40 || true
	echo "VERDICT: K3s did NOT become Ready unprivileged; see log above"
	exit 1
fi
