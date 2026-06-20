package localnet

import "testing"

func TestNetIPResolverRejectsEmptyInterface(t *testing.T) {
	_, err := (NetIPResolver{}).IPv4ByInterface("")
	if err == nil {
		t.Fatal("expected empty interface error")
	}
}
