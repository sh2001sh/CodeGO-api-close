package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/internal/adminops/infra/ionet"
)

// GetDeploymentSettings returns whether io.net deployment support is enabled and configured.
func GetDeploymentSettings() DeploymentSettings {
	_, enabled, configured := getDeploymentAPIKey()
	return DeploymentSettings{
		Provider:   "io.net",
		Enabled:    enabled,
		Configured: configured,
		CanConnect: enabled && configured,
	}
}

// TestDeploymentConnection validates the configured or provided io.net API key.
func TestDeploymentConnection(apiKeyOverride string) (*DeploymentConnectionResult, error) {
	apiKey := strings.TrimSpace(apiKeyOverride)
	if apiKey == "" {
		storedKey, _, configured := getDeploymentAPIKey()
		if !configured {
			return nil, ErrDeploymentAPIKeyRequired
		}
		apiKey = storedKey
	}

	client := newDeploymentEnterpriseClient(apiKey)
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
func ListDeployments(page int, pageSize int, status string) (map[string]any, error) {
	client, err := getDeploymentEnterpriseClient()
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
func SearchDeployments(page int, pageSize int, status string, keyword string) (map[string]any, error) {
	client, err := getDeploymentEnterpriseClient()
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
func GetDeploymentDetails(id string) (map[string]any, error) {
	client, err := getDeploymentEnterpriseClient()
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
func ListHardwareTypes() (map[string]any, error) {
	client, err := getDeploymentEnterpriseClient()
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
func ListLocations() (map[string]any, error) {
	client, err := getDeploymentPublicClient()
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
func GetAvailableReplicas(hardwareID int, gpuCount int) (*ionet.AvailableReplicasResponse, error) {
	client, err := getDeploymentEnterpriseClient()
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
func GetPriceEstimation(req *ionet.PriceEstimationRequest) (*ionet.PriceEstimationResponse, error) {
	client, err := getDeploymentEnterpriseClient()
	if err != nil {
		return nil, err
	}
	return client.GetPriceEstimation(req)
}

// CheckClusterNameAvailability reports whether a cluster name is available.
func CheckClusterNameAvailability(name string) (map[string]any, error) {
	client, err := getDeploymentEnterpriseClient()
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

// GetDeploymentLogs returns raw logs for a specific container in a deployment.
func GetDeploymentLogs(id string, containerID string, opts *ionet.GetLogsOptions) (string, error) {
	client, err := getDeploymentPublicClient()
	if err != nil {
		return "", err
	}
	deploymentID, err := requireDeploymentID(id)
	if err != nil {
		return "", err
	}
	trimmedContainerID := strings.TrimSpace(containerID)
	if trimmedContainerID == "" {
		return "", ErrDeploymentContainerQueryMiss
	}
	return client.GetContainerLogsRaw(deploymentID, trimmedContainerID, opts)
}

// ListDeploymentContainers returns containers for a deployment.
func ListDeploymentContainers(id string) (map[string]any, error) {
	client, err := getDeploymentEnterpriseClient()
	if err != nil {
		return nil, err
	}
	deploymentID, err := requireDeploymentID(id)
	if err != nil {
		return nil, err
	}

	containers, err := client.ListContainers(deploymentID)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0)
	if containers != nil {
		items = make([]map[string]any, 0, len(containers.Workers))
		for _, container := range containers.Workers {
			events := make([]map[string]any, 0, len(container.ContainerEvents))
			for _, event := range container.ContainerEvents {
				events = append(events, map[string]any{
					"time":    event.Time.Unix(),
					"message": event.Message,
				})
			}
			items = append(items, map[string]any{
				"container_id":       container.ContainerID,
				"device_id":          container.DeviceID,
				"status":             strings.ToLower(strings.TrimSpace(container.Status)),
				"hardware":           container.Hardware,
				"brand_name":         container.BrandName,
				"created_at":         container.CreatedAt.Unix(),
				"uptime_percent":     container.UptimePercent,
				"gpus_per_container": container.GPUsPerContainer,
				"public_url":         container.PublicURL,
				"events":             events,
			})
		}
	}

	response := map[string]any{
		"total":      0,
		"containers": items,
	}
	if containers != nil {
		response["total"] = containers.Total
	}
	return response, nil
}

// GetContainerDetails returns details for a specific deployment container.
func GetContainerDetails(id string, containerID string) (map[string]any, error) {
	client, err := getDeploymentEnterpriseClient()
	if err != nil {
		return nil, err
	}
	deploymentID, err := requireDeploymentID(id)
	if err != nil {
		return nil, err
	}
	requiredContainerID, err := requireContainerID(containerID)
	if err != nil {
		return nil, err
	}

	details, err := client.GetContainerDetails(deploymentID, requiredContainerID)
	if err != nil {
		return nil, err
	}
	if details == nil {
		return nil, fmt.Errorf("container details not found")
	}

	events := make([]map[string]any, 0, len(details.ContainerEvents))
	for _, event := range details.ContainerEvents {
		events = append(events, map[string]any{
			"time":    event.Time.Unix(),
			"message": event.Message,
		})
	}
	return map[string]any{
		"deployment_id":      deploymentID,
		"container_id":       details.ContainerID,
		"device_id":          details.DeviceID,
		"status":             strings.ToLower(strings.TrimSpace(details.Status)),
		"hardware":           details.Hardware,
		"brand_name":         details.BrandName,
		"created_at":         details.CreatedAt.Unix(),
		"uptime_percent":     details.UptimePercent,
		"gpus_per_container": details.GPUsPerContainer,
		"public_url":         details.PublicURL,
		"events":             events,
	}, nil
}

// BuildDeploymentLogOptions converts query parameters into io.net log options.
func BuildDeploymentLogOptions(level string, stream string, limit int, cursor string, follow bool, startTime string, endTime string) *ionet.GetLogsOptions {
	opts := &ionet.GetLogsOptions{
		Level:  level,
		Stream: stream,
		Limit:  limit,
		Cursor: cursor,
		Follow: follow,
	}
	if parsed, err := time.Parse(time.RFC3339, startTime); err == nil {
		opts.StartTime = &parsed
	}
	if parsed, err := time.Parse(time.RFC3339, endTime); err == nil {
		opts.EndTime = &parsed
	}
	return opts
}
