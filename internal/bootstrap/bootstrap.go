package bootstrap

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

type Spec struct {
	AgentConfig       string
	AgentBinaryPath   string
	AgentBinaryURL    string
	ConfigPath        string
	LoxiCMDBinaryPath string
	LoxiCMDBinaryURL  string
	LoxiLBImage       string
	LoxiLBContainer   string
	PrimaryInterface  string
	MetricsPort       int
}

func RenderUserData(spec Spec) (string, error) {
	spec = withDefaults(spec)
	if strings.TrimSpace(spec.AgentConfig) == "" {
		return "", fmt.Errorf("agent config is required")
	}
	var out bytes.Buffer
	tpl, err := template.New("user-data").Funcs(template.FuncMap{
		"shellQuote": shellQuote,
	}).Parse(userDataTemplate)
	if err != nil {
		return "", fmt.Errorf("parse user data template: %w", err)
	}
	if err := tpl.Execute(&out, spec); err != nil {
		return "", fmt.Errorf("render user data: %w", err)
	}
	return out.String(), nil
}

func withDefaults(spec Spec) Spec {
	if spec.AgentBinaryPath == "" {
		spec.AgentBinaryPath = "/usr/local/bin/betternat-agent"
	}
	if spec.LoxiCMDBinaryPath == "" {
		spec.LoxiCMDBinaryPath = "/usr/local/bin/loxicmd"
	}
	if spec.ConfigPath == "" {
		spec.ConfigPath = "/etc/betternat/agent.json"
	}
	if spec.LoxiLBImage == "" {
		spec.LoxiLBImage = "ghcr.io/loxilb-io/loxilb:latest"
	}
	if spec.LoxiLBContainer == "" {
		spec.LoxiLBContainer = "loxilb"
	}
	if spec.PrimaryInterface == "" {
		spec.PrimaryInterface = "ens5"
	}
	if spec.MetricsPort == 0 {
		spec.MetricsPort = 9108
	}
	return spec
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

const userDataTemplate = `#!/bin/bash
set -euo pipefail

install_package() {
  if command -v dnf >/dev/null 2>&1; then
    dnf install -y "$@"
  elif command -v yum >/dev/null 2>&1; then
    yum install -y "$@"
  elif command -v apt-get >/dev/null 2>&1; then
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y "$@"
  else
    echo "no supported package manager found" >&2
    exit 1
  fi
}

if ! command -v curl >/dev/null 2>&1; then
  install_package curl ca-certificates
fi

if ! command -v docker >/dev/null 2>&1; then
  install_package docker
fi

{{- if .AgentBinaryURL }}
curl -fsSL {{ shellQuote .AgentBinaryURL }} -o {{ shellQuote .AgentBinaryPath }}
chmod 0755 {{ shellQuote .AgentBinaryPath }}
{{- end }}

{{- if .LoxiCMDBinaryURL }}
curl -fsSL {{ shellQuote .LoxiCMDBinaryURL }} -o {{ shellQuote .LoxiCMDBinaryPath }}
chmod 0755 {{ shellQuote .LoxiCMDBinaryPath }}
{{- end }}

install -d -m 0755 /etc/betternat
cat > {{ shellQuote .ConfigPath }} <<'BETTERNAT_AGENT_CONFIG'
{{ .AgentConfig }}
BETTERNAT_AGENT_CONFIG
chmod 0600 {{ shellQuote .ConfigPath }}

cat > /etc/sysctl.d/99-betternat.conf <<'BETTERNAT_SYSCTL'
net.ipv4.ip_forward = 1
net.ipv4.conf.all.rp_filter = 0
net.ipv4.conf.default.rp_filter = 0
net.netfilter.nf_conntrack_max = 1048576
BETTERNAT_SYSCTL
sysctl --system

systemctl enable --now docker
docker rm -f {{ .LoxiLBContainer }} >/dev/null 2>&1 || true
docker run -d \
  --name {{ .LoxiLBContainer }} \
  --restart unless-stopped \
  --privileged \
  --network host \
  -v /lib/modules:/lib/modules:ro \
  {{ .LoxiLBImage }}

if ! command -v loxicmd >/dev/null 2>&1; then
  cat > {{ shellQuote .LoxiCMDBinaryPath }} <<BETTERNAT_LOXICMD_WRAPPER
#!/bin/sh
exec docker exec {{ .LoxiLBContainer }} loxicmd "\$@"
BETTERNAT_LOXICMD_WRAPPER
  chmod 0755 {{ shellQuote .LoxiCMDBinaryPath }}
fi

cat > /etc/systemd/system/betternat-agent.service <<'BETTERNAT_AGENT_SERVICE'
[Unit]
Description=BetterNAT Agent
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
ExecStart={{ .AgentBinaryPath }} --config {{ .ConfigPath }}
Restart=always
RestartSec=2s
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
BETTERNAT_AGENT_SERVICE

systemctl daemon-reload
systemctl enable --now betternat-agent.service
`
