package ionet

import "time"

// APIError represents an API error response
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// Error implements the error interface
func (e *APIError) Error() string {
	if e.Details != "" {
		return e.Message + ": " + e.Details
	}
	return e.Message
}

// ListDeploymentsOptions represents options for listing deployments
type ListDeploymentsOptions struct {
	Status     string `json:"status,omitempty"`      // filter by status
	LocationID int    `json:"location_id,omitempty"` // filter by location
	Page       int    `json:"page,omitempty"`        // pagination
	PageSize   int    `json:"page_size,omitempty"`   // pagination
	SortBy     string `json:"sort_by,omitempty"`     // sort field
	SortOrder  string `json:"sort_order,omitempty"`  // asc/desc
}

// GetLogsOptions represents options for retrieving container logs
type GetLogsOptions struct {
	StartTime *time.Time `json:"start_time,omitempty"`
	EndTime   *time.Time `json:"end_time,omitempty"`
	Level     string     `json:"level,omitempty"`  // filter by log level
	Stream    string     `json:"stream,omitempty"` // filter by stdout/stderr streams
	Limit     int        `json:"limit,omitempty"`  // max number of log entries
	Cursor    string     `json:"cursor,omitempty"` // pagination cursor
	Follow    bool       `json:"follow,omitempty"` // stream logs
}

// HardwareType represents a hardware type available for deployment
type HardwareType struct {
	ID             int     `json:"id"`
	Name           string  `json:"name"`
	Description    string  `json:"description,omitempty"`
	GPUType        string  `json:"gpu_type"`
	GPUMemory      int     `json:"gpu_memory"` // in GB
	MaxGPUs        int     `json:"max_gpus"`
	CPU            string  `json:"cpu,omitempty"`
	Memory         int     `json:"memory,omitempty"`  // in GB
	Storage        int     `json:"storage,omitempty"` // in GB
	HourlyRate     float64 `json:"hourly_rate"`
	Available      bool    `json:"available"`
	BrandName      string  `json:"brand_name,omitempty"`
	AvailableCount int     `json:"available_count,omitempty"`
}

// Location represents a deployment location
type Location struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	ISO2        string  `json:"iso2,omitempty"`
	Region      string  `json:"region,omitempty"`
	Country     string  `json:"country,omitempty"`
	Latitude    float64 `json:"latitude,omitempty"`
	Longitude   float64 `json:"longitude,omitempty"`
	Available   int     `json:"available,omitempty"`
	Description string  `json:"description,omitempty"`
}

// LocationsResponse represents the list of locations and aggregated metadata.
type LocationsResponse struct {
	Locations []Location `json:"locations"`
	Total     int        `json:"total"`
}

// LocationAvailability represents real-time availability for a location
type LocationAvailability struct {
	LocationID           int                    `json:"location_id"`
	LocationName         string                 `json:"location_name"`
	Available            bool                   `json:"available"`
	HardwareAvailability []HardwareAvailability `json:"hardware_availability"`
	UpdatedAt            time.Time              `json:"updated_at"`
}

// HardwareAvailability represents availability for specific hardware at a location
type HardwareAvailability struct {
	HardwareID     int    `json:"hardware_id"`
	HardwareName   string `json:"hardware_name"`
	AvailableCount int    `json:"available_count"`
	MaxGPUs        int    `json:"max_gpus"`
}
