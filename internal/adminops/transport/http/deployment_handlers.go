package http

import (
	"errors"
	httpapi "github.com/sh2001sh/new-api/internal/platform/transport/http/httpapi"
	"strconv"

	"github.com/gin-gonic/gin"

	adminopsapp "github.com/sh2001sh/new-api/internal/adminops/app"
	"github.com/sh2001sh/new-api/internal/adminops/infra/ionet"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
)

// GetModelDeploymentSettings returns io.net deployment configuration status.

func GetModelDeploymentSettings(c *gin.Context) {
	httpapi.ApiSuccess(c, adminopsapp.GetDeploymentSettings())
}

// TestIoNetConnection validates a provided or stored io.net API key.
func TestIoNetConnection(c *gin.Context) {
	rawBody, err := c.GetRawData()
	if err != nil {
		httpapi.ApiError(c, err)
		return
	}
	apiKey, err := adminopsapp.ParseConnectionAPIKey(rawBody)
	if err != nil {
		httpapi.ApiErrorMsg(c, "invalid request payload")
		return
	}

	result, err := adminopsapp.TestDeploymentConnection(apiKey)
	if err != nil {
		if apiErr, ok := err.(*ionet.APIError); ok {
			message := apiErr.Message
			if message == "" {
				message = "failed to validate api key"
			}
			httpapi.ApiErrorMsg(c, message)
			return
		}
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, result)
}

// GetAllDeployments returns paginated deployments.
func GetAllDeployments(c *gin.Context) {
	pageInfo := platformpagination.GetPageQuery(c)
	data, err := adminopsapp.ListDeployments(pageInfo.GetPage(), pageInfo.GetPageSize(), c.Query("status"))
	if err != nil {
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, data)
}

// SearchDeployments returns paginated deployments filtered by keyword.
func SearchDeployments(c *gin.Context) {
	pageInfo := platformpagination.GetPageQuery(c)
	data, err := adminopsapp.SearchDeployments(pageInfo.GetPage(), pageInfo.GetPageSize(), c.Query("status"), c.Query("keyword"))
	if err != nil {
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, data)
}

// GetDeployment returns detailed deployment information.
func GetDeployment(c *gin.Context) {
	data, err := adminopsapp.GetDeploymentDetails(c.Param("id"))
	if err != nil {
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, data)
}

// UpdateDeploymentName updates the deployment cluster name.
func UpdateDeploymentName(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	data, err := adminopsapp.UpdateDeploymentName(c.Param("id"), req.Name)
	if err != nil {
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, data)
}

// UpdateDeployment updates the deployment runtime configuration.
func UpdateDeployment(c *gin.Context) {
	var req ionet.UpdateDeploymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	data, err := adminopsapp.UpdateDeployment(c.Param("id"), &req)
	if err != nil {
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, data)
}

// ExtendDeployment extends the deployment duration.
func ExtendDeployment(c *gin.Context) {
	var req ionet.ExtendDurationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	data, err := adminopsapp.ExtendDeployment(c.Param("id"), &req)
	if err != nil {
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, data)
}

// DeleteDeployment requests deployment termination.
func DeleteDeployment(c *gin.Context) {
	data, err := adminopsapp.DeleteDeployment(c.Param("id"))
	if err != nil {
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, data)
}

// CreateDeployment creates a new deployment.
func CreateDeployment(c *gin.Context) {
	var req ionet.DeploymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	data, err := adminopsapp.CreateDeployment(&req)
	if err != nil {
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, data)
}

// GetHardwareTypes returns available io.net hardware types.
func GetHardwareTypes(c *gin.Context) {
	data, err := adminopsapp.ListHardwareTypes()
	if err != nil {
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, data)
}

// GetLocations returns available io.net deployment locations.
func GetLocations(c *gin.Context) {
	data, err := adminopsapp.ListLocations()
	if err != nil {
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, data)
}

// GetAvailableReplicas returns available replicas for a hardware type.
func GetAvailableReplicas(c *gin.Context) {
	hardwareIDValue := c.Query("hardware_id")
	if hardwareIDValue == "" {
		handleDeploymentError(c, adminopsapp.ErrDeploymentHardwareIDRequired)
		return
	}
	hardwareID, err := strconv.Atoi(hardwareIDValue)
	if err != nil || hardwareID <= 0 {
		handleDeploymentError(c, adminopsapp.ErrDeploymentHardwareIDInvalid)
		return
	}

	gpuCount := 1
	if gpuCountValue := c.Query("gpu_count"); gpuCountValue != "" {
		if parsed, err := strconv.Atoi(gpuCountValue); err == nil && parsed > 0 {
			gpuCount = parsed
		}
	}

	replicas, err := adminopsapp.GetAvailableReplicas(hardwareID, gpuCount)
	if err != nil {
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, replicas)
}

// GetPriceEstimation returns a price estimation for a deployment request.
func GetPriceEstimation(c *gin.Context) {
	var req ionet.PriceEstimationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpapi.ApiError(c, err)
		return
	}
	data, err := adminopsapp.GetPriceEstimation(&req)
	if err != nil {
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, data)
}

// CheckClusterNameAvailability checks whether a deployment name is available.
func CheckClusterNameAvailability(c *gin.Context) {
	data, err := adminopsapp.CheckClusterNameAvailability(c.Query("name"))
	if err != nil {
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, data)
}

// GetDeploymentLogs returns raw deployment container logs.
func GetDeploymentLogs(c *gin.Context) {
	limit := 100
	if limitValue := c.Query("limit"); limitValue != "" {
		if parsed, err := strconv.Atoi(limitValue); err == nil && parsed > 0 {
			limit = parsed
			if limit > 1000 {
				limit = 1000
			}
		}
	}
	opts := adminopsapp.BuildDeploymentLogOptions(
		c.Query("level"),
		c.Query("stream"),
		limit,
		c.Query("cursor"),
		c.Query("follow") == "true",
		c.Query("start_time"),
		c.Query("end_time"),
	)
	logs, err := adminopsapp.GetDeploymentLogs(c.Param("id"), c.Query("container_id"), opts)
	if err != nil {
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, logs)
}

// ListDeploymentContainers returns containers for a deployment.
func ListDeploymentContainers(c *gin.Context) {
	data, err := adminopsapp.ListDeploymentContainers(c.Param("id"))
	if err != nil {
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, data)
}

// GetContainerDetails returns details for a deployment container.
func GetContainerDetails(c *gin.Context) {
	data, err := adminopsapp.GetContainerDetails(c.Param("id"), c.Param("container_id"))
	if err != nil {
		handleDeploymentError(c, err)
		return
	}
	httpapi.ApiSuccess(c, data)
}

func handleDeploymentError(c *gin.Context, err error) {
	switch {
	case err == nil:
		return
	case errors.Is(err, adminopsapp.ErrDeploymentProviderUnavailable),
		errors.Is(err, adminopsapp.ErrDeploymentAPIKeyRequired),
		errors.Is(err, adminopsapp.ErrDeploymentIDRequired),
		errors.Is(err, adminopsapp.ErrDeploymentContainerIDRequired),
		errors.Is(err, adminopsapp.ErrDeploymentNameRequired),
		errors.Is(err, adminopsapp.ErrDeploymentNameUnavailable),
		errors.Is(err, adminopsapp.ErrDeploymentNameQueryRequired),
		errors.Is(err, adminopsapp.ErrDeploymentHardwareIDRequired),
		errors.Is(err, adminopsapp.ErrDeploymentHardwareIDInvalid),
		errors.Is(err, adminopsapp.ErrDeploymentContainerQueryMiss):
		httpapi.ApiErrorMsg(c, err.Error())
	default:
		httpapi.ApiError(c, err)
	}
}
