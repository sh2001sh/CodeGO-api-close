package http

import (
	"fmt"
	"github.com/gin-gonic/gin"
	adminopsapp "github.com/sh2001sh/new-api/internal/adminops/app"
	"github.com/sh2001sh/new-api/internal/adminops/infra/ionet"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	stdhttp "net/http"
	"testing"
)

type fakeDeploymentClient struct {
	maxGPUResponse        *ionet.MaxGPUResponse
	maxGPUErr             error
	nameAvailable         bool
	nameAvailabilityErr   error
	updateClusterNameResp *ionet.UpdateClusterNameResponse
	updateClusterNameErr  error
}

func (f *fakeDeploymentClient) GetMaxGPUsPerContainer() (*ionet.MaxGPUResponse, error) {
	return f.maxGPUResponse, f.maxGPUErr
}
func (f *fakeDeploymentClient) ListDeployments(opts *ionet.ListDeploymentsOptions) (*ionet.DeploymentList, error) {
	return nil, fmt.Errorf("unexpected ListDeployments call")
}
func (f *fakeDeploymentClient) GetDeployment(deploymentID string) (*ionet.DeploymentDetail, error) {
	return nil, fmt.Errorf("unexpected GetDeployment call")
}
func (f *fakeDeploymentClient) UpdateClusterName(clusterID string, req *ionet.UpdateClusterNameRequest) (*ionet.UpdateClusterNameResponse, error) {
	return f.updateClusterNameResp, f.updateClusterNameErr
}
func (f *fakeDeploymentClient) UpdateDeployment(deploymentID string, req *ionet.UpdateDeploymentRequest) (*ionet.UpdateDeploymentResponse, error) {
	return nil, fmt.Errorf("unexpected UpdateDeployment call")
}
func (f *fakeDeploymentClient) ExtendDeployment(deploymentID string, req *ionet.ExtendDurationRequest) (*ionet.DeploymentDetail, error) {
	return nil, fmt.Errorf("unexpected ExtendDeployment call")
}
func (f *fakeDeploymentClient) DeleteDeployment(deploymentID string) (*ionet.UpdateDeploymentResponse, error) {
	return nil, fmt.Errorf("unexpected DeleteDeployment call")
}
func (f *fakeDeploymentClient) DeployContainer(req *ionet.DeploymentRequest) (*ionet.DeploymentResponse, error) {
	return nil, fmt.Errorf("unexpected DeployContainer call")
}
func (f *fakeDeploymentClient) ListHardwareTypes() ([]ionet.HardwareType, int, error) {
	return nil, 0, fmt.Errorf("unexpected ListHardwareTypes call")
}
func (f *fakeDeploymentClient) ListLocations() (*ionet.LocationsResponse, error) {
	return nil, fmt.Errorf("unexpected ListLocations call")
}
func (f *fakeDeploymentClient) GetAvailableReplicas(hardwareID int, gpuCount int) (*ionet.AvailableReplicasResponse, error) {
	return nil, fmt.Errorf("unexpected GetAvailableReplicas call")
}
func (f *fakeDeploymentClient) GetPriceEstimation(req *ionet.PriceEstimationRequest) (*ionet.PriceEstimationResponse, error) {
	return nil, fmt.Errorf("unexpected GetPriceEstimation call")
}
func (f *fakeDeploymentClient) CheckClusterNameAvailability(clusterName string) (bool, error) {
	return f.nameAvailable, f.nameAvailabilityErr
}
func (f *fakeDeploymentClient) GetContainerLogsRaw(deploymentID, containerID string, opts *ionet.GetLogsOptions) (string, error) {
	return "", fmt.Errorf("unexpected GetContainerLogsRaw call")
}
func (f *fakeDeploymentClient) ListContainers(deploymentID string) (*ionet.ContainerList, error) {
	return nil, fmt.Errorf("unexpected ListContainers call")
}
func (f *fakeDeploymentClient) GetContainerDetails(deploymentID, containerID string) (*ionet.Container, error) {
	return nil, fmt.Errorf("unexpected GetContainerDetails call")
}

func TestGetModelDeploymentSettingsReflectsOptions(t *testing.T) {
	setupAdminOpsHTTPTestDB(t)

	platformconfig.OptionMap["model_deployment.ionet.enabled"] = "true"
	platformconfig.OptionMap["model_deployment.ionet.api_key"] = "secret-key"

	ctx, recorder := newAdminOpsContext(t, stdhttp.MethodGet, "/api/deployments/settings", nil)
	GetModelDeploymentSettings(ctx)

	response := decodeAdminOpsResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected deployment settings to succeed, got %#v", response)
	}
	var payload struct {
		Provider   string `json:"provider"`
		Enabled    bool   `json:"enabled"`
		Configured bool   `json:"configured"`
		CanConnect bool   `json:"can_connect"`
	}
	if err := platformencoding.Unmarshal(response.Data, &payload); err != nil {
		t.Fatalf("failed to decode deployment settings: %v", err)
	}
	if payload.Provider != "io.net" || !payload.Enabled || !payload.Configured || !payload.CanConnect {
		t.Fatalf("unexpected deployment settings payload: %#v", payload)
	}
}

func TestTestIoNetConnectionUsesProvidedAPIKey(t *testing.T) {
	setupAdminOpsHTTPTestDB(t)

	restore := adminopsapp.SetDeploymentClientFactoriesForTest(
		func(apiKey string) adminopsapp.DeploymentClientForTest {
			return &fakeDeploymentClient{}
		},
		func(apiKey string) adminopsapp.DeploymentClientForTest {
			if apiKey != "override-key" {
				t.Fatalf("expected override API key, got %q", apiKey)
			}
			return &fakeDeploymentClient{
				maxGPUResponse: &ionet.MaxGPUResponse{
					Hardware: []ionet.MaxGPUInfo{{Available: 2}, {Available: 3}},
				},
			}
		},
	)
	t.Cleanup(restore)

	ctx, recorder := newAdminOpsContext(t, stdhttp.MethodPost, "/api/deployments/test-connection", map[string]any{
		"api_key": "override-key",
	})
	TestIoNetConnection(ctx)

	response := decodeAdminOpsResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected connection test to succeed, got %#v", response)
	}
	var payload struct {
		HardwareCount  int `json:"hardware_count"`
		TotalAvailable int `json:"total_available"`
	}
	if err := platformencoding.Unmarshal(response.Data, &payload); err != nil {
		t.Fatalf("failed to decode test connection payload: %v", err)
	}
	if payload.HardwareCount != 2 || payload.TotalAvailable != 5 {
		t.Fatalf("unexpected connection payload: %#v", payload)
	}
}

func TestUpdateDeploymentNameRejectsUnavailableName(t *testing.T) {
	setupAdminOpsHTTPTestDB(t)

	platformconfig.OptionMap["model_deployment.ionet.enabled"] = "true"
	platformconfig.OptionMap["model_deployment.ionet.api_key"] = "stored-key"

	restore := adminopsapp.SetDeploymentClientFactoriesForTest(
		func(apiKey string) adminopsapp.DeploymentClientForTest { return &fakeDeploymentClient{} },
		func(apiKey string) adminopsapp.DeploymentClientForTest {
			return &fakeDeploymentClient{nameAvailable: false}
		},
	)
	t.Cleanup(restore)

	ctx, recorder := newAdminOpsContext(t, stdhttp.MethodPut, "/api/deployments/deploy-1/name", map[string]any{
		"name": "taken-name",
	})
	ctx.Params = gin.Params{{Key: "id", Value: "deploy-1"}}
	UpdateDeploymentName(ctx)

	response := decodeAdminOpsResponse(t, recorder)
	if response.Success {
		t.Fatalf("expected update deployment name to fail when name is unavailable")
	}
}
