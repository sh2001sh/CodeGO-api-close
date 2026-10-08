package adminops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/adminops/ionet"
)

// GetDeploymentLogs returns raw logs for a specific container in a deployment.
func (s *Server) GetDeploymentLogs(ctx context.Context, id string, containerID string, opts *ionet.GetLogsOptions) (string, error) {
	client, err := s.deploymentClient(ctx, true)
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
func (s *Server) ListDeploymentContainers(ctx context.Context, id string) (map[string]any, error) {
	client, err := s.deploymentClient(ctx, false)
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
func (s *Server) GetContainerDetails(ctx context.Context, id string, containerID string) (map[string]any, error) {
	client, err := s.deploymentClient(ctx, false)
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
func (s *Server) BuildDeploymentLogOptions(ctx context.Context, level string, stream string, limit int, cursor string, follow bool, startTime string, endTime string) *ionet.GetLogsOptions {
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
