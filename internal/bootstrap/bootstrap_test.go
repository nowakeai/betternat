package bootstrap

import (
	"strings"
	"testing"
)

func TestRenderUserData(t *testing.T) {
	script, err := RenderUserData(Spec{
		AgentConfig: "version: v0\ngateway_id: prod-egress\n",
		LoxiLBImage: "ghcr.io/loxilb-io/loxilb:v0.99.7",
	})
	if err != nil {
		t.Fatalf("render user data: %v", err)
	}

	assertContains(t, script, "chmod 0600 '/etc/betternat/agent.json'")
	assertContains(t, script, "install_package docker")
	assertContains(t, script, "net.ipv4.ip_forward = 1")
	assertContains(t, script, "net.netfilter.nf_conntrack_max = 1048576")
	assertContains(t, script, "docker run -d")
	assertContains(t, script, "ghcr.io/loxilb-io/loxilb:v0.99.7")
	assertContains(t, script, `exec docker exec loxilb loxicmd "\$@"`)
	assertContains(t, script, "ExecStart=/usr/local/bin/betternat-agent --config /etc/betternat/agent.json")
	assertContains(t, script, "systemctl enable --now betternat-agent.service")
}

func TestRenderUserDataWithBinaryURLs(t *testing.T) {
	script, err := RenderUserData(Spec{
		AgentConfig:      `{"version":"v0","gateway_id":"prod-egress"}`,
		AgentBinaryURL:   "https://example.invalid/betternat-agent?date=20260620T123230Z&signature=abc",
		LoxiCMDBinaryURL: "https://example.invalid/loxicmd?download=1&token=abc",
	})
	if err != nil {
		t.Fatalf("render user data: %v", err)
	}

	assertContains(t, script, "curl -fsSL 'https://example.invalid/betternat-agent?date=20260620T123230Z&signature=abc' -o '/usr/local/bin/betternat-agent'")
	assertContains(t, script, "chmod 0755 '/usr/local/bin/betternat-agent'")
	assertContains(t, script, "curl -fsSL 'https://example.invalid/loxicmd?download=1&token=abc' -o '/usr/local/bin/loxicmd'")
	assertContains(t, script, "chmod 0755 '/usr/local/bin/loxicmd'")
}

func TestRenderUserDataRequiresConfig(t *testing.T) {
	_, err := RenderUserData(Spec{})
	if err == nil {
		t.Fatal("expected missing config error")
	}
}

func assertContains(t *testing.T, text string, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Fatalf("missing %q in:\n%s", want, text)
	}
}
