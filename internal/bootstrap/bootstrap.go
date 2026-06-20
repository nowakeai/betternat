package bootstrap

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

type Spec struct {
	AgentConfig      string
	AgentBinaryPath  string
	ConfigPath       string
	LoxiLBImage      string
	LoxiLBContainer  string
	PrimaryInterface string
	MetricsPort      int
}

func RenderUserData(spec Spec) (string, error) {
	spec = withDefaults(spec)
	if strings.TrimSpace(spec.AgentConfig) == "" {
		return "", fmt.Errorf("agent config is required")
	}
	var out bytes.Buffer
	if err := userDataTemplate.Execute(&out, spec); err != nil {
		return "", fmt.Errorf("render user data: %w", err)
	}
	return out.String(), nil
}

func withDefaults(spec Spec) Spec {
	if spec.AgentBinaryPath == "" {
		spec.AgentBinaryPath = "/usr/local/bin/betternat-agent"
	}
	if spec.ConfigPath == "" {
		spec.ConfigPath = "/etc/betternat/agent.yaml"
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

var userDataTemplate = template.Must(template.New("user-data").Parse(`#!/bin/bash
set -euo pipefail

install -d -m 0755 /etc/betternat
cat > {{ .ConfigPath }} <<'BETTERNAT_AGENT_CONFIG'
{{ .AgentConfig }}
BETTERNAT_AGENT_CONFIG
chmod 0600 {{ .ConfigPath }}

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
`))
