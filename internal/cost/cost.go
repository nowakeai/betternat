package cost

import "fmt"

type EstimateInput struct {
	ProcessedGB               float64 `json:"processed_gb"`
	Hours                     float64 `json:"hours"`
	NATGatewayHourlyUSD       float64 `json:"nat_gateway_hourly_usd"`
	NATGatewayProcessingUSDGB float64 `json:"nat_gateway_processing_usd_per_gb"`
	ApplianceHourlyUSD        float64 `json:"appliance_hourly_usd"`
	ApplianceCount            int     `json:"appliance_count"`
}

type Estimate struct {
	ProcessedGB         float64 `json:"processed_gb"`
	NATGatewayUSD       float64 `json:"nat_gateway_usd"`
	BetterNATUSD        float64 `json:"betternat_usd"`
	EstimatedSavingsUSD float64 `json:"estimated_savings_usd"`
	SavingsPercent      float64 `json:"savings_percent"`
}

func DefaultInput() EstimateInput {
	return EstimateInput{
		Hours:                     730,
		NATGatewayHourlyUSD:       0.045,
		NATGatewayProcessingUSDGB: 0.045,
		ApplianceHourlyUSD:        0.05,
		ApplianceCount:            2,
	}
}

func EstimateMonthly(input EstimateInput) (Estimate, error) {
	if input.ProcessedGB < 0 {
		return Estimate{}, fmt.Errorf("processed gb must be non-negative")
	}
	if input.Hours <= 0 {
		return Estimate{}, fmt.Errorf("hours must be positive")
	}
	if input.NATGatewayHourlyUSD < 0 || input.NATGatewayProcessingUSDGB < 0 || input.ApplianceHourlyUSD < 0 {
		return Estimate{}, fmt.Errorf("prices must be non-negative")
	}
	if input.ApplianceCount <= 0 {
		return Estimate{}, fmt.Errorf("appliance count must be positive")
	}
	natGateway := input.Hours*input.NATGatewayHourlyUSD + input.ProcessedGB*input.NATGatewayProcessingUSDGB
	betterNAT := input.Hours * input.ApplianceHourlyUSD * float64(input.ApplianceCount)
	savings := natGateway - betterNAT
	percent := 0.0
	if natGateway > 0 {
		percent = savings / natGateway * 100
	}
	return Estimate{
		ProcessedGB:         input.ProcessedGB,
		NATGatewayUSD:       natGateway,
		BetterNATUSD:        betterNAT,
		EstimatedSavingsUSD: savings,
		SavingsPercent:      percent,
	}, nil
}
