package adminops

import (
	"context"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/adminops/ionet"
)

// TestDeploymentConnection validates the configured or provided io.net API key.
func (s *Server) TestDeploymentConnection(ctx context.Context, apiKeyOverride string) (*DeploymentConnectionResult, error) {
	apiKey := strings.TrimSpace(apiKeyOverride)
	if apiKey == "" {
		storedKey, _, configured, err := s.deploymentAPIKey(ctx)
		if err != nil {
			return nil, err
		}
		if !configured {
			return nil, ErrDeploymentAPIKeyRequired
		}
		apiKey = storedKey
	}

	client := s.makeDeploymentClient(ctx, apiKey, false)
	result, err := client.GetMaxGPUsPerContainer()
	if err != nil {
		return nil, err
	}

	connection := &DeploymentConnectionResult{}
	if result != nil {
		connection.HardwareCount = len(result.Hardware)
		connection.TotalAvailable = result.Total
		if connection.TotalAvailable == 0 {
			for _, hardware := range result.Hardware {
				connection.TotalAvailable += hardware.Available
			}
		}
	}
	return connection, nil
}

// ListDeployments returns paginated deployment data and derived status counts.
func (s *Server) ListDeployments(ctx context.Context, page int, pageSize int, status string) (map[string]any, error) {
	client, err := s.deploymentClient(ctx, false)
	if err != nil {
		return nil, err
	}

	list, err := client.ListDeployments(&ionet.ListDeploymentsOptions{
		Status:    strings.ToLower(strings.TrimSpace(status)),
		Page:      page,
		PageSize:  pageSize,
		SortBy:    "created_at",
		SortOrder: "desc",
	})
	if err != nil {
		return nil, err
	}

	items := make([]map[string]any, 0, len(list.Deployments))
	for _, deployment := range list.Deployments {
		items = append(items, mapIoNetDeployment(deployment))
	}

	return map[string]any{
		"page":          page,
		"page_size":     pageSize,
		"total":         list.Total,
		"items":         items,
		"status_counts": computeDeploymentStatusCounts(list.Total, list.Deployments),
	}, nil
}

// SearchDeployments returns paginated deployment data filtered by status and optional keyword.
func (s *Server) SearchDeployments(ctx context.Context, page int, pageSize int, status string, keyword string) (map[string]any, error) {
	client, err := s.deploymentClient(ctx, false)
	if err != nil {
		return nil, err
	}

	list, err := client.ListDeployments(&ionet.ListDeploymentsOptions{
		Status:    strings.ToLower(strings.TrimSpace(status)),
		Page:      page,
		PageSize:  pageSize,
		SortBy:    "created_at",
		SortOrder: "desc",
	})
	if err != nil {
		return nil, err
	}

	trimmedKeyword := strings.ToLower(strings.TrimSpace(keyword))
	filtered := make([]ionet.Deployment, 0, len(list.Deployments))
	if trimmedKeyword == "" {
		filtered = list.Deployments
	} else {
		for _, deployment := range list.Deployments {
			if strings.Contains(strings.ToLower(deployment.Name), trimmedKeyword) {
				filtered = append(filtered, deployment)
			}
		}
	}

	items := make([]map[string]any, 0, len(filtered))
	for _, deployment := range filtered {
		items = append(items, mapIoNetDeployment(deployment))
	}

	total := list.Total
	if trimmedKeyword != "" {
		total = len(filtered)
	}
	return map[string]any{
		"page":      page,
		"page_size": pageSize,
		"total":     total,
		"items":     items,
	}, nil
}

// GetDeploymentDetails returns detailed deployment information.
func (s *Server) GetDeploymentDetails(ctx context.Context, id string) (map[string]any, error) {
	client, err := s.deploymentClient(ctx, false)
	if err != nil {
		return nil, err
	}
	deploymentID, err := requireDeploymentID(id)
	if err != nil {
		return nil, err
	}

	details, err := client.GetDeployment(deploymentID)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"id":              details.ID,
		"deployment_name": details.ID,
		"model_name":      "",
		"model_version":   "",
		"status":          strings.ToLower(details.Status),
		"instance_count":  details.TotalContainers,
		"hardware_id":     details.HardwareID,
		"resource_config": map[string]any{
			"cpu":    "",
			"memory": "",
			"gpu":    strconv.Itoa(details.TotalGPUs),
		},
		"created_at":                details.CreatedAt.Unix(),
		"updated_at":                details.CreatedAt.Unix(),
		"description":               "",
		"amount_paid":               details.AmountPaid,
		"completed_percent":         details.CompletedPercent,
		"gpus_per_container":        details.GPUsPerContainer,
		"total_gpus":                details.TotalGPUs,
		"total_containers":          details.TotalContainers,
		"hardware_name":             details.HardwareName,
		"brand_name":                details.BrandName,
		"compute_minutes_served":    details.ComputeMinutesServed,
		"compute_minutes_remaining": details.ComputeMinutesRemaining,
		"locations":                 details.Locations,
		"container_config":          details.ContainerConfig,
	}, nil
}

// ListHardwareTypes returns the available hardware types and totals.
func (s *Server) ListHardwareTypes(ctx context.Context) (map[string]any, error) {
	client, err := s.deploymentClient(ctx, false)
	if err != nil {
		return nil, err
	}
	hardwareTypes, totalAvailable, err := client.ListHardwareTypes()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"hardware_types":  hardwareTypes,
		"total":           len(hardwareTypes),
		"total_available": totalAvailable,
	}, nil
}

// ListLocations returns the available deployment locations.
func (s *Server) ListLocations(ctx context.Context) (map[string]any, error) {
	client, err := s.deploymentClient(ctx, true)
	if err != nil {
		return nil, err
	}
	locationsResp, err := client.ListLocations()
	if err != nil {
		return nil, err
	}

	total := locationsResp.Total
	if total == 0 {
		total = len(locationsResp.Locations)
	}
	return map[string]any{
		"locations": locationsResp.Locations,
		"total":     total,
	}, nil
}

// GetAvailableReplicas returns available replica counts for the given hardware and GPU quantity.
func (s *Server) GetAvailableReplicas(ctx context.Context, hardwareID int, gpuCount int) (*ionet.AvailableReplicasResponse, error) {
	client, err := s.deploymentClient(ctx, false)
	if err != nil {
		return nil, err
	}
	if hardwareID == 0 {
		return nil, ErrDeploymentHardwareIDRequired
	}
	if hardwareID < 0 {
		return nil, ErrDeploymentHardwareIDInvalid
	}
	if gpuCount <= 0 {
		gpuCount = 1
	}
	return client.GetAvailableReplicas(hardwareID, gpuCount)
}

// GetPriceEstimation returns an io.net price estimation for a deployment request.
func (s *Server) GetPriceEstimation(ctx context.Context, req *ionet.PriceEstimationRequest) (*ionet.PriceEstimationResponse, error) {
	client, err := s.deploymentClient(ctx, false)
	if err != nil {
		return nil, err
	}
	return client.GetPriceEstimation(req)
}

// CheckClusterNameAvailability reports whether a cluster name is available.
func (s *Server) CheckClusterNameAvailability(ctx context.Context, name string) (map[string]any, error) {
	client, err := s.deploymentClient(ctx, false)
	if err != nil {
		return nil, err
	}
	clusterName := strings.TrimSpace(name)
	if clusterName == "" {
		return nil, ErrDeploymentNameQueryRequired
	}
	available, err := client.CheckClusterNameAvailability(clusterName)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"available": available,
		"name":      clusterName,
	}, nil
}
