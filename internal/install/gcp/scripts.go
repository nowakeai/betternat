package gcp

import (
	"fmt"
	"strings"
)

const defaultPrivateCIDR = "10.0.0.0/8"

type StartupScriptInputs struct {
	PrivateCIDRs []string
}

func GatewayStartupScript(inputs StartupScriptInputs) string {
	cidrs := inputs.PrivateCIDRs
	if len(cidrs) == 0 {
		cidrs = []string{defaultPrivateCIDR}
	}
	var rules strings.Builder
	for _, cidr := range cidrs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		fmt.Fprintf(&rules, "nft add rule ip nat postrouting ip saddr %s masquerade || true\n", cidr)
	}
	return fmt.Sprintf(`#!/bin/bash
set -euxo pipefail
exec > >(tee /var/log/betternat-gcp-gateway-startup.log | logger -t betternat-gcp-gateway-startup) 2>&1
sysctl -w net.ipv4.ip_forward=1
printf 'net.ipv4.ip_forward=1\n' >/etc/sysctl.d/99-betternat-forward.conf
apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y nftables curl
systemctl enable --now nftables || true
nft list table ip nat >/dev/null 2>&1 || nft add table ip nat
nft list chain ip nat postrouting >/dev/null 2>&1 || nft 'add chain ip nat postrouting { type nat hook postrouting priority srcnat; policy accept; }'
%scurl -fsS --max-time 10 https://checkip.amazonaws.com | tee /var/log/betternat-gcp-gateway-egress-ip.txt || true
`, rules.String())
}

func ClientVerifyStartupScript() string {
	return `#!/bin/bash
set -euxo pipefail
exec >/dev/console 2>&1
echo "BETTERNAT_GCP_VERIFY_START $(date -Is)"
ip route
for i in $(seq 1 30); do
  if python3 - <<'PY'
import urllib.request
print('BETTERNAT_GCP_EGRESS_IP=' + urllib.request.urlopen('https://checkip.amazonaws.com', timeout=10).read().decode().strip())
PY
  then
    echo "BETTERNAT_GCP_VERIFY_OK $(date -Is)"
    exit 0
  fi
  echo "BETTERNAT_GCP_VERIFY_RETRY=$i"
  sleep 10
done
echo "BETTERNAT_GCP_VERIFY_FAILED $(date -Is)"
exit 1
`
}
