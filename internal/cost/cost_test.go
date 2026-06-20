package cost

import "testing"

func TestEstimateMonthly(t *testing.T) {
	input := DefaultInput()
	input.ProcessedGB = 50 * 1024
	estimate, err := EstimateMonthly(input)
	if err != nil {
		t.Fatalf("estimate monthly: %v", err)
	}
	wantNATGateway := 730*0.045 + (50 * 1024 * 0.045)
	wantBetterNAT := 730 * 0.05 * 2
	if estimate.NATGatewayUSD != wantNATGateway {
		t.Fatalf("nat gateway cost = %f want %f", estimate.NATGatewayUSD, wantNATGateway)
	}
	if estimate.BetterNATUSD != wantBetterNAT {
		t.Fatalf("betternat cost = %f want %f", estimate.BetterNATUSD, wantBetterNAT)
	}
	if estimate.EstimatedSavingsUSD <= 0 {
		t.Fatalf("expected savings: %#v", estimate)
	}
}

func TestEstimateMonthlyRejectsInvalidInput(t *testing.T) {
	input := DefaultInput()
	input.ProcessedGB = -1
	if _, err := EstimateMonthly(input); err == nil {
		t.Fatal("expected invalid input error")
	}
}
