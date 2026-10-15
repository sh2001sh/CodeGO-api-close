package app

import (
	"fmt"
	"strings"

	"github.com/sh2001sh/new-api/internal/adminops/infra/ionet"
)

// UpdateDeploymentName updates the name of a deployment after checking availability.
func UpdateDeploymentName(id string, name string) (map[string]any, error) {
	client, err := getDeploymentEnterpriseClient()
	if err != nil {
		return nil, err
	}
	deploymentID, err := requireDeploymentID(id)
	if err != nil {
		return nil, err
	}

	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return nil, ErrDeploymentNameRequired
	}
	available, err := client.CheckClusterNameAvailability(trimmedName)
	if err != nil {
		return nil, fmt.Errorf("failed to check name availability: %w", err)
	}
	if !available {
		return nil, ErrDeploymentNameUnavailable
	}

	resp, err := client.UpdateClusterName(deploymentID, &ionet.UpdateClusterNameRequest{Name: trimmedName})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"status":  resp.Status,
		"message": resp.Message,
		"id":      deploymentID,
		"name":    trimmedName,
	}, nil
}

// UpdateDeployment updates an existing deployment configuration.
func UpdateDeployment(id string, req *ionet.UpdateDeploymentRequest) (map[string]any, error) {
	client, err := getDeploymentEnterpriseClient()
	if err != nil {
		return nil, err
	}
	deploymentID, err := requireDeploymentID(id)
	if err != nil {
		return nil, err
	}

	resp, err := client.UpdateDeployment(deploymentID, req)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"status":        resp.Status,
		"deployment_id": resp.DeploymentID,
	}, nil
}

// ExtendDeployment extends an existing deployment duration.
func ExtendDeployment(id string, req *ionet.ExtendDurationRequest) (map[string]any, error) {
	client, err := getDeploymentEnterpriseClient()
	if err != nil {
		return nil, err
	}
	deploymentID, err := requireDeploymentID(id)
	if err != nil {
		return nil, err
	}

	details, err := client.ExtendDeployment(deploymentID, req)
	if err != nil {
		return nil, err
	}
	return mapIoNetDeployment(ionet.Deployment{
		ID:                      details.ID,
		Status:                  details.Status,
		Name:                    deploymentID,
		CompletedPercent:        float64(details.CompletedPercent),
		HardwareQuantity:        details.TotalGPUs,
		BrandName:               details.BrandName,
		HardwareName:            details.HardwareName,
		ComputeMinutesServed:    details.ComputeMinutesServed,
		ComputeMinutesRemaining: details.ComputeMinutesRemaining,
		CreatedAt:               details.CreatedAt,
	}), nil
}

// DeleteDeployment requests deployment termination.
func DeleteDeployment(id string) (map[string]any, error) {
	client, err := getDeploymentEnterpriseClient()
	if err != nil {
		return nil, err
	}
	deploymentID, err := requireDeploymentID(id)
	if err != nil {
		return nil, err
	}

	resp, err := client.DeleteDeployment(deploymentID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"status":        resp.Status,
		"deployment_id": resp.DeploymentID,
		"message":       "Deployment termination requested successfully",
	}, nil
}

// CreateDeployment creates a new deployment.
func CreateDeployment(req *ionet.DeploymentRequest) (map[string]any, error) {
	client, err := getDeploymentEnterpriseClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.DeployContainer(req)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"deployment_id": resp.DeploymentID,
		"status":        resp.Status,
		"message":       "Deployment created successfully",
	}, nil
}
