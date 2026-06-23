#!/usr/bin/env bash
set -euo pipefail

BETTERNAT_VERSION="${BETTERNAT_VERSION:-dev}"
BETTERNAT_LOXILB_RUNTIME="${BETTERNAT_LOXILB_RUNTIME:-systemd}"
BETTERNAT_LOXILB_DEB_URL="${BETTERNAT_LOXILB_DEB_URL:-}"
BETTERNAT_LOXILB_DEB_SHA256="${BETTERNAT_LOXILB_DEB_SHA256:-}"

if [ "$BETTERNAT_LOXILB_RUNTIME" != "systemd" ]; then
  echo "unsupported Ubuntu LoxiLB runtime: $BETTERNAT_LOXILB_RUNTIME" >&2
  exit 1
fi

if [ -z "$BETTERNAT_LOXILB_DEB_URL" ]; then
  echo "BETTERNAT_LOXILB_DEB_URL is required for Ubuntu systemd AMIs" >&2
  exit 1
fi

wait_for_apt() {
  cloud-init status --wait >/dev/null 2>&1 || true
  while fuser /var/lib/dpkg/lock-frontend /var/lib/dpkg/lock /var/lib/apt/lists/lock /var/cache/apt/archives/lock >/dev/null 2>&1; do
    sleep 5
  done
}

install -m 0755 /tmp/betternat-agent /usr/local/bin/betternat-agent
install -m 0755 /tmp/betternat /usr/local/bin/betternat

export DEBIAN_FRONTEND=noninteractive
wait_for_apt
apt-get update
wait_for_apt
apt-get upgrade -y
wait_for_apt
apt-get install -y \
  ca-certificates \
  conntrack \
  curl \
  golang-go \
  gzip \
  iproute2 \
  iptables \
  jq \
  nftables \
  procps \
  tar

curl -fsSL "$BETTERNAT_LOXILB_DEB_URL" -o /tmp/loxilb.deb
if [ -n "$BETTERNAT_LOXILB_DEB_SHA256" ]; then
  printf '%s  /tmp/loxilb.deb\n' "$BETTERNAT_LOXILB_DEB_SHA256" | sha256sum -c -
fi

apt-get install -y /tmp/loxilb.deb
rm -f /tmp/loxilb.deb

install -d -m 0755 /etc/betternat
install -d -m 0755 /etc/systemd/system/loxilb.service.d
install -d -m 0755 /usr/local/lib/betternat
install -d -m 0755 /usr/share/doc/betternat/licenses
install -d -m 0755 /usr/share/doc/betternat/licenses/loxilb

if [ -f /tmp/betternat-LICENSE ]; then
  install -m 0644 /tmp/betternat-LICENSE /usr/share/doc/betternat/LICENSE
  install -m 0644 /tmp/betternat-LICENSE /usr/share/doc/betternat/licenses/Apache-2.0.txt
  install -m 0644 /tmp/betternat-LICENSE /usr/share/doc/betternat/licenses/loxilb/LICENSE
fi

if [ -f /tmp/betternat-THIRD_PARTY_NOTICES.md ]; then
  install -m 0644 /tmp/betternat-THIRD_PARTY_NOTICES.md /usr/share/doc/betternat/THIRD_PARTY_NOTICES.md
fi

cat > /etc/systemd/system/loxilb.service.d/betternat.conf <<'EOF'
[Service]
ExecStart=
ExecStart=/usr/local/sbin/loxilb --api --fallback
EOF

cat > /etc/systemd/system/betternat-agent.service <<'EOF'
[Unit]
Description=BetterNAT Agent
After=network-online.target loxilb.service
Wants=network-online.target
Requires=loxilb.service

[Service]
Type=simple
ExecStart=/usr/local/bin/betternat-agent --config /etc/betternat/agent.json
Restart=always
RestartSec=2s
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
EOF

cat > /etc/sysctl.d/99-betternat.conf <<'EOF'
net.ipv4.ip_forward = 1
net.ipv4.conf.all.rp_filter = 0
net.ipv4.conf.default.rp_filter = 0
EOF

if [ -e /proc/sys/net/netfilter/nf_conntrack_max ]; then
  echo "net.netfilter.nf_conntrack_max = 1048576" >> /etc/sysctl.d/99-betternat.conf
fi

cat > /usr/share/doc/betternat/AMI_MANIFEST <<EOF
BetterNATVersion=$BETTERNAT_VERSION
LoxiLBRuntime=systemd
LoxiLBDebURL=$BETTERNAT_LOXILB_DEB_URL
BuiltAt=$(date -u +%Y-%m-%dT%H:%M:%SZ)
BaseOS=$(source /etc/os-release && printf '%s %s' "$NAME" "$VERSION_ID")
EOF

systemctl daemon-reload
systemctl enable loxilb.service
systemctl enable betternat-agent.service

if systemctl list-unit-files amazon-ssm-agent.service >/dev/null 2>&1; then
  systemctl enable amazon-ssm-agent.service
elif systemctl list-unit-files snap.amazon-ssm-agent.amazon-ssm-agent.service >/dev/null 2>&1; then
  systemctl enable snap.amazon-ssm-agent.amazon-ssm-agent.service
fi

/usr/local/bin/betternat version
/usr/local/bin/betternat-agent --version
command -v loxicmd
systemctl is-enabled loxilb.service
