package app

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/sh2001sh/new-api/internal/adminops/infra/ionet"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"strconv"
	"strings"
	"time"
)

var (
	ErrDeploymentProviderUnavailable = errors.New("io.net model deployment is not enabled or api key missing")
	ErrDeploymentAPIKeyRequired      = errors.New("api_key is required")
	ErrDeploymentIDRequired          = errors.New("deployment ID is required")
	ErrDeploymentContainerIDRequired = errors.New("container ID is required")
	ErrDeploymentNameRequired        = errors.New("deployment name cannot be empty")
	ErrDeploymentNameUnavailable     = errors.New("deployment name is not available, please choose a different name")
	ErrDeploymentNameQueryRequired   = errors.New("name parameter is required")
	ErrDeploymentHardwareIDRequired  = errors.New("hardware_id parameter is required")
	ErrDeploymentHardwareIDInvalid   = errors.New("invalid hardware_id parameter")
	ErrDeploymentContainerQueryMiss  = errors.New("container_id parameter is required")
)

type ioNetAPI interface {
	GetMaxGPUsPerContainer() (*ionet.MaxGPUResponse, error)
	ListDeployments(opts *ionet.ListDeploymentsOptions) (*ionet.DeploymentList, error)
	GetDeployment(deploymentID string) (*ionet.DeploymentDetail, error)
	UpdateClusterName(clusterID string, req *ionet.UpdateClusterNameRequest) (*ionet.UpdateClusterNameResponse, error)
	UpdateDeployment(deploymentID string, req *ionet.UpdateDeploymentRequest) (*ionet.UpdateDeploymentResponse, error)
	ExtendDeployment(deploymentID string, req *ionet.ExtendDurationRequest) (*ionet.DeploymentDetail, error)
	DeleteDeployment(deploymentID string) (*ionet.UpdateDeploymentResponse, error)
	DeployContainer(req *ionet.DeploymentRequest) (*ionet.DeploymentResponse, error)
	ListHardwareTypes() ([]ionet.HardwareType, int, error)
	ListLocations() (*ionet.LocationsResponse, error)
	GetAvailableReplicas(hardwareID int, gpuCount int) (*ionet.AvailableReplicasResponse, error)
	GetPriceEstimation(req *ionet.PriceEstimationRequest) (*ionet.PriceEstimationResponse, error)
	CheckClusterNameAvailability(clusterName string) (bool, error)
	GetContainerLogsRaw(deploymentID, containerID string, opts *ionet.GetLogsOptions) (string, error)
	ListContainers(deploymentID string) (*ionet.ContainerList, error)
	GetContainerDetails(deploymentID, containerID string) (*ionet.Container, error)
}

var (
	newDeploymentPublicClient = func(apiKey string) ioNetAPI {
		return ionet.NewClient(apiKey)
	}
	newDeploymentEnterpriseClient = func(apiKey string) ioNetAPI {
		return ionet.NewEnterpriseClient(apiKey)
	}
)

// DeploymentSettings summarizes whether io.net deployment support is configured.
type DeploymentSettings struct {
	Provider   string `json:"provider"`
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
	CanConnect bool   `json:"can_connect"`
}

// DeploymentConnectionResult contains basic connectivity stats for io.net.
type DeploymentConnectionResult struct {
	HardwareCount  int `json:"hardware_count"`
	TotalAvailable int `json:"total_available"`
}

func getDeploymentAPIKey() (string, bool, bool) {
	platformconfig.OptionMapRWMutex.RLock()
	enabled := platformconfig.OptionMap["model_deployment.ionet.enabled"] == "true"
	apiKey := strings.TrimSpace(platformconfig.OptionMap["model_deployment.ionet.api_key"])
	platformconfig.OptionMapRWMutex.RUnlock()
	return apiKey, enabled, apiKey != ""
}

func getDeploymentEnterpriseClient() (ioNetAPI, error) {
	apiKey, enabled, configured := getDeploymentAPIKey()
	if !enabled || !configured {
		return nil, ErrDeploymentProviderUnavailable
	}
	return newDeploymentEnterpriseClient(apiKey), nil
}

func getDeploymentPublicClient() (ioNetAPI, error) {
	apiKey, enabled, configured := getDeploymentAPIKey()
	if !enabled || !configured {
		return nil, ErrDeploymentProviderUnavailable
	}
	return newDeploymentPublicClient(apiKey), nil
}

func requireDeploymentID(id string) (string, error) {
	deploymentID := strings.TrimSpace(id)
	if deploymentID == "" {
		return "", ErrDeploymentIDRequired
	}
	return deploymentID, nil
}

func requireContainerID(id string) (string, error) {
	containerID := strings.TrimSpace(id)
	if containerID == "" {
		return "", ErrDeploymentContainerIDRequired
	}
	return containerID, nil
}

func mapIoNetDeployment(d ionet.Deployment) map[string]any {
	var created int64
	if d.CreatedAt.IsZero() {
		created = time.Now().Unix()
	} else {
		created = d.CreatedAt.Unix()
	}

	timeRemainingHours := d.ComputeMinutesRemaining / 60
	timeRemainingMins := d.ComputeMinutesRemaining % 60
	timeRemaining := "completed"
	if timeRemainingHours > 0 {
		timeRemaining = fmt.Sprintf("%d hour %d minutes", timeRemainingHours, timeRemainingMins)
	} else if timeRemainingMins > 0 {
		timeRemaining = fmt.Sprintf("%d minutes", timeRemainingMins)
	}

	hardwareInfo := fmt.Sprintf("%s %s x%d", d.BrandName, d.HardwareName, d.HardwareQuantity)

	return map[string]any{
		"id":                        d.ID,
		"deployment_name":           d.Name,
		"container_name":            d.Name,
		"status":                    strings.ToLower(d.Status),
		"type":                      "Container",
		"time_remaining":            timeRemaining,
		"time_remaining_minutes":    d.ComputeMinutesRemaining,
		"hardware_info":             hardwareInfo,
		"hardware_name":             d.HardwareName,
		"brand_name":                d.BrandName,
		"hardware_quantity":         d.HardwareQuantity,
		"completed_percent":         d.CompletedPercent,
		"compute_minutes_served":    d.ComputeMinutesServed,
		"compute_minutes_remaining": d.ComputeMinutesRemaining,
		"created_at":                created,
		"updated_at":                created,
		"model_name":                "",
		"model_version":             "",
		"instance_count":            d.HardwareQuantity,
		"resource_config": map[string]any{
			"cpu":    "",
			"memory": "",
			"gpu":    strconv.Itoa(d.HardwareQuantity),
		},
		"description": "",
		"provider":    "io.net",
	}
}

func computeDeploymentStatusCounts(total int, deployments []ionet.Deployment) map[string]int64 {
	counts := map[string]int64{"all": int64(total)}
	for _, status := range []string{"running", "completed", "failed", "deployment requested", "termination requested", "destroyed"} {
		counts[status] = 0
	}
	for _, deployment := range deployments {
		status := strings.ToLower(strings.TrimSpace(deployment.Status))
		counts[status]++
	}
	return counts
}

func parseConnectionAPIKey(rawBody []byte) (string, error) {
	var req struct {
		APIKey string `json:"api_key"`
	}
	if len(bytes.TrimSpace(rawBody)) == 0 {
		return "", nil
	}
	if err := platformencoding.Unmarshal(rawBody, &req); err != nil {
		return "", err
	}
	return strings.TrimSpace(req.APIKey), nil
}
