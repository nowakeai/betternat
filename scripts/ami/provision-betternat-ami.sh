#!/usr/bin/env bash
set -euo pipefail

BETTERNAT_VERSION="${BETTERNAT_VERSION:-dev}"
BETTERNAT_LOXILB_IMAGE="${BETTERNAT_LOXILB_IMAGE:-ghcr.io/loxilb-io/loxilb@sha256:dacc9b21688d4042b768f2cbc5968360b8753cf92f926ee288346153a23f3052}"

install -m 0755 /tmp/betternat-agent /usr/local/bin/betternat-agent
install -m 0755 /tmp/betternat /usr/local/bin/betternat

dnf update -y
dnf install -y \
  amazon-cloudwatch-agent \
  amazon-ssm-agent \
  ca-certificates \
  conntrack-tools \
  docker \
  gzip \
  iproute \
  iptables \
  jq \
  nftables \
  procps-ng \
  tar

systemctl enable docker
systemctl start docker
systemctl enable amazon-ssm-agent

docker pull "$BETTERNAT_LOXILB_IMAGE"

install -d -m 0755 /etc/betternat
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

cat > /usr/local/bin/loxicmd <<'LOXICMD'
#!/usr/bin/env bash
set -euo pipefail
exec docker exec loxilb loxicmd "$@"
LOXICMD
chmod 0755 /usr/local/bin/loxicmd

cat > /etc/systemd/system/loxilb.service <<EOF
[Unit]
Description=LoxiLB datapath for BetterNAT
After=docker.service
Requires=docker.service

[Service]
Type=simple
Restart=always
RestartSec=2s
ExecStartPre=-/usr/bin/docker rm -f loxilb
ExecStart=/usr/bin/docker run --rm --name loxilb --privileged --network host $BETTERNAT_LOXILB_IMAGE --api --fallback
ExecStop=/usr/bin/docker stop loxilb

[Install]
WantedBy=multi-user.target
EOF

cat > /etc/systemd/system/betternat-agent.service <<'EOF'
[Unit]
Description=BetterNAT Agent
After=network-online.target docker.service loxilb.service
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
LoxiLBImage=$BETTERNAT_LOXILB_IMAGE
BuiltAt=$(date -u +%Y-%m-%dT%H:%M:%SZ)
BaseOS=$(source /etc/os-release && printf '%s %s' "$NAME" "$VERSION_ID")
EOF

systemctl daemon-reload
systemctl enable loxilb.service
systemctl enable betternat-agent.service

/usr/local/bin/betternat version
/usr/local/bin/betternat-agent --version
systemctl is-enabled docker
systemctl is-enabled amazon-ssm-agent
