package ionet

import (
	"fmt"
	"strings"
)

// GetPriceEstimation calculates the estimated cost for a deployment
func (c *Client) GetPriceEstimation(req *PriceEstimationRequest) (*PriceEstimationResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("price estimation request cannot be nil")
	}

	// Validate required fields
	if len(req.LocationIDs) == 0 {
		return nil, fmt.Errorf("location_ids is required")
	}
	if req.HardwareID == 0 {
		return nil, fmt.Errorf("hardware_id is required")
	}
	if req.ReplicaCount < 1 {
		return nil, fmt.Errorf("replica_count must be at least 1")
	}

	currency := strings.TrimSpace(req.Currency)
	if currency == "" {
		currency = "usdc"
	}

	durationType := strings.TrimSpace(req.DurationType)
	if durationType == "" {
		durationType = "hour"
	}
	durationType = strings.ToLower(durationType)

	apiDurationType := ""

	durationQty := req.DurationQty
	if durationQty < 1 {
		durationQty = req.DurationHours
	}
	if durationQty < 1 {
		return nil, fmt.Errorf("duration_qty must be at least 1")
	}

	hardwareQty := req.HardwareQty
	if hardwareQty < 1 {
		hardwareQty = req.GPUsPerContainer
	}
	if hardwareQty < 1 {
		return nil, fmt.Errorf("hardware_qty must be at least 1")
	}

	durationHoursForRate := req.DurationHours
	if durationHoursForRate < 1 {
		durationHoursForRate = durationQty
	}
	switch durationType {
	case "hour", "hours", "hourly":
		durationHoursForRate = durationQty
		apiDurationType = "hourly"
	case "day", "days", "daily":
		durationHoursForRate = durationQty * 24
		apiDurationType = "daily"
	case "week", "weeks", "weekly":
		durationHoursForRate = durationQty * 24 * 7
		apiDurationType = "weekly"
	case "month", "months", "monthly":
		durationHoursForRate = durationQty * 24 * 30
		apiDurationType = "monthly"
	}
	if durationHoursForRate < 1 {
		durationHoursForRate = 1
	}
	if apiDurationType == "" {
		apiDurationType = "hourly"
	}

	params := map[string]interface{}{
		"location_ids":       req.LocationIDs,
		"hardware_id":        req.HardwareID,
		"hardware_qty":       hardwareQty,
		"gpus_per_container": req.GPUsPerContainer,
		"duration_type":      apiDurationType,
		"duration_qty":       durationQty,
		"duration_hours":     req.DurationHours,
		"replica_count":      req.ReplicaCount,
		"currency":           currency,
	}

	endpoint := "/price" + buildQueryParams(params)

	resp, err := c.makeRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get price estimation: %w", err)
	}

	// Parse according to the actual API response format from docs:
	// {
	//   "data": {
	//     "replica_count": 0,
	//     "gpus_per_container": 0,
	//     "available_replica_count": [0],
	//     "discount": 0,
	//     "ionet_fee": 0,
	//     "ionet_fee_percent": 0,
	//     "currency_conversion_fee": 0,
	//     "currency_conversion_fee_percent": 0,
	//     "total_cost_usdc": 0
	//   }
	// }
	var pricingData struct {
		ReplicaCount                 int     `json:"replica_count"`
		GPUsPerContainer             int     `json:"gpus_per_container"`
		AvailableReplicaCount        []int   `json:"available_replica_count"`
		Discount                     float64 `json:"discount"`
		IonetFee                     float64 `json:"ionet_fee"`
		IonetFeePercent              float64 `json:"ionet_fee_percent"`
		CurrencyConversionFee        float64 `json:"currency_conversion_fee"`
		CurrencyConversionFeePercent float64 `json:"currency_conversion_fee_percent"`
		TotalCostUSDC                float64 `json:"total_cost_usdc"`
	}

	if err := decodeData(resp.Body, &pricingData); err != nil {
		return nil, fmt.Errorf("failed to parse price estimation response: %w", err)
	}

	// Convert to our internal format
	durationHoursFloat := float64(durationHoursForRate)
	if durationHoursFloat <= 0 {
		durationHoursFloat = 1
	}

	priceResp := &PriceEstimationResponse{
		EstimatedCost:   pricingData.TotalCostUSDC,
		Currency:        strings.ToUpper(currency),
		EstimationValid: true,
		PriceBreakdown: PriceBreakdown{
			ComputeCost: pricingData.TotalCostUSDC - pricingData.IonetFee - pricingData.CurrencyConversionFee,
			TotalCost:   pricingData.TotalCostUSDC,
			HourlyRate:  pricingData.TotalCostUSDC / durationHoursFloat,
		},
	}

	return priceResp, nil
}
