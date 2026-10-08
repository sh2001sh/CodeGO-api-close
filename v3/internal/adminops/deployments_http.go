package adminops

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/adminops/ionet"
)

func (s *Server) registerDeployments(mux *http.ServeMux, auth Authenticate) {
	routes := map[string]actorHandler{
		"GET /api/deployments/settings":                  s.deploymentSettingsHTTP,
		"POST /api/deployments/settings/test-connection": s.deploymentConnectionHTTP,
		"POST /api/deployments/test-connection":          s.deploymentConnectionHTTP,
		"GET /api/deployments/{$}":                       s.deploymentListHTTP,
		"GET /api/deployments/search":                    s.deploymentListHTTP,
		"GET /api/deployments/hardware-types": func(w http.ResponseWriter, r *http.Request, _ Actor) {
			v, e := s.ListHardwareTypes(r.Context())
			s.deploymentReply(w, v, e)
		},
		"GET /api/deployments/locations": func(w http.ResponseWriter, r *http.Request, _ Actor) {
			v, e := s.ListLocations(r.Context())
			s.deploymentReply(w, v, e)
		},
		"GET /api/deployments/available-replicas":             s.deploymentReplicasHTTP,
		"POST /api/deployments/price-estimation":              s.deploymentPriceHTTP,
		"GET /api/deployments/check-name":                     s.deploymentNameCheckHTTP,
		"POST /api/deployments/{$}":                           s.deploymentCreateHTTP,
		"GET /api/deployments/{id}":                           s.deploymentGetHTTP,
		"PUT /api/deployments/{id}":                           s.deploymentUpdateHTTP,
		"PUT /api/deployments/{id}/name":                      s.deploymentRenameHTTP,
		"POST /api/deployments/{id}/extend":                   s.deploymentExtendHTTP,
		"DELETE /api/deployments/{id}":                        s.deploymentDeleteHTTP,
		"GET /api/deployments/{id}/logs":                      s.deploymentLogsHTTP,
		"GET /api/deployments/{id}/containers":                s.deploymentContainersHTTP,
		"GET /api/deployments/{id}/containers/{container_id}": s.deploymentContainerHTTP,
	}
	for p, h := range routes {
		mux.HandleFunc(p, s.protected(auth, "admin", h))
	}
}
func (s *Server) deploymentSettingsHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	_, enabled, configured, err := s.deploymentAPIKey(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, DeploymentSettings{Provider: "io.net", Enabled: enabled, Configured: configured, CanConnect: enabled && configured})
}
func (s *Server) deploymentConnectionHTTP(w http.ResponseWriter, r *http.Request, a Actor) {
	var in struct {
		APIKey string `json:"api_key"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	if in.APIKey != "" && a.Role != "root" {
		fail(w, 403, "forbidden", "Only root may test a replacement credential")
		return
	}
	if len(in.APIKey) > 4096 || strings.ContainsAny(in.APIKey, "\r\n") {
		fail(w, 400, "invalid_credential", "Invalid deployment credential")
		return
	}
	v, e := s.TestDeploymentConnection(r.Context(), in.APIKey)
	s.deploymentReply(w, v, e)
}
func (s *Server) deploymentListHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	p, n, ok := pagination(w, r)
	if !ok {
		return
	}
	if r.URL.Path == "/api/deployments/search" {
		v, e := s.SearchDeployments(r.Context(), p, n, r.URL.Query().Get("status"), r.URL.Query().Get("keyword"))
		s.deploymentReply(w, v, e)
		return
	}
	v, e := s.ListDeployments(r.Context(), p, n, r.URL.Query().Get("status"))
	s.deploymentReply(w, v, e)
}
func (s *Server) deploymentGetHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	v, e := s.GetDeploymentDetails(r.Context(), r.PathValue("id"))
	s.deploymentReply(w, v, e)
}
func (s *Server) deploymentDeleteHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	v, e := s.DeleteDeployment(r.Context(), r.PathValue("id"))
	s.deploymentReply(w, v, e)
}
func (s *Server) deploymentContainersHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	v, e := s.ListDeploymentContainers(r.Context(), r.PathValue("id"))
	s.deploymentReply(w, v, e)
}
func (s *Server) deploymentContainerHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	v, e := s.GetContainerDetails(r.Context(), r.PathValue("id"), r.PathValue("container_id"))
	s.deploymentReply(w, v, e)
}
func (s *Server) deploymentCreateHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	var in ionet.DeploymentRequest
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.ResourcePrivateName) == "" || len(in.ResourcePrivateName) > 255 || in.HardwareID <= 0 || len(in.LocationIDs) == 0 || len(in.LocationIDs) > 100 || in.DurationHours < 1 || in.DurationHours > 87600 || in.GPUsPerContainer < 1 || in.GPUsPerContainer > 1024 || in.ContainerConfig.ReplicaCount < 1 || in.ContainerConfig.ReplicaCount > 10000 || strings.TrimSpace(in.RegistryConfig.ImageURL) == "" || in.ContainerConfig.TrafficPort < 0 || in.ContainerConfig.TrafficPort > 65535 {
		fail(w, 400, "invalid_deployment", "Invalid required deployment fields")
		return
	}
	for _, id := range in.LocationIDs {
		if id <= 0 {
			fail(w, 400, "invalid_location", "Expected positive location IDs")
			return
		}
	}
	v, e := s.CreateDeployment(r.Context(), &in)
	s.deploymentReply(w, v, e)
}
func (s *Server) deploymentUpdateHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	var in ionet.UpdateDeploymentRequest
	if !decode(w, r, &in) {
		return
	}
	if in.TrafficPort != nil && (*in.TrafficPort < 1 || *in.TrafficPort > 65535) {
		fail(w, 400, "invalid_deployment", "Invalid replica count or traffic port")
		return
	}
	v, e := s.UpdateDeployment(r.Context(), r.PathValue("id"), &in)
	s.deploymentReply(w, v, e)
}
func (s *Server) deploymentExtendHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	var in ionet.ExtendDurationRequest
	if !decode(w, r, &in) {
		return
	}
	if in.DurationHours < 1 || in.DurationHours > 87600 {
		fail(w, 400, "invalid_duration", "Invalid extension duration")
		return
	}
	v, e := s.ExtendDeployment(r.Context(), r.PathValue("id"), &in)
	s.deploymentReply(w, v, e)
}
func (s *Server) deploymentRenameHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 255 {
		fail(w, 400, "invalid_name", "Invalid deployment name")
		return
	}
	v, e := s.UpdateDeploymentName(r.Context(), r.PathValue("id"), in.Name)
	s.deploymentReply(w, v, e)
}
func (s *Server) deploymentNameCheckHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	name := r.URL.Query().Get("name")
	if strings.TrimSpace(name) == "" || len(name) > 255 {
		fail(w, 400, "invalid_name", "Invalid deployment name")
		return
	}
	v, e := s.CheckClusterNameAvailability(r.Context(), name)
	s.deploymentReply(w, v, e)
}
func (s *Server) deploymentReplicasHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	hardware, e := strconv.Atoi(r.URL.Query().Get("hardware_id"))
	gpu := 1
	if raw := r.URL.Query().Get("gpu_count"); raw != "" {
		var err error
		gpu, err = strconv.Atoi(raw)
		if err != nil {
			gpu = 0
		}
	}
	if e != nil || hardware < 1 || gpu < 1 || gpu > 1024 {
		fail(w, 400, "invalid_hardware", "Expected positive hardware ID and GPU count")
		return
	}
	v, err := s.GetAvailableReplicas(r.Context(), hardware, gpu)
	s.deploymentReply(w, v, err)
}
func (s *Server) deploymentPriceHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	var in ionet.PriceEstimationRequest
	if !decode(w, r, &in) {
		return
	}
	if in.HardwareID <= 0 || len(in.LocationIDs) == 0 || in.ReplicaCount < 1 || in.ReplicaCount > 10000 || in.DurationQty < 1 && in.DurationHours < 1 || in.DurationQty > 87600 || in.DurationHours > 87600 || in.HardwareQty < 1 && in.GPUsPerContainer < 1 || in.HardwareQty > 1024 || in.GPUsPerContainer > 1024 {
		fail(w, 400, "invalid_estimation", "Invalid price estimation fields")
		return
	}
	v, e := s.GetPriceEstimation(r.Context(), &in)
	s.deploymentReply(w, v, e)
}
func (s *Server) deploymentLogsHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	q := r.URL.Query()
	id := q.Get("container_id")
	if !validProviderID(id) {
		fail(w, 400, "invalid_container", "Container ID is required")
		return
	}
	limit := 100
	if raw := q.Get("limit"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n < 1 || n > 1000 {
			fail(w, 400, "invalid_limit", "Expected limit between 1 and 1000")
			return
		}
		limit = n
	}
	for _, key := range []string{"start_time", "end_time"} {
		if q.Get(key) != "" {
			if _, e := time.Parse(time.RFC3339, q.Get(key)); e != nil {
				fail(w, 400, "invalid_time", "Expected RFC3339 timestamps")
				return
			}
		}
	}
	opts := s.BuildDeploymentLogOptions(r.Context(), q.Get("level"), q.Get("stream"), limit, q.Get("cursor"), q.Get("follow") == "true", q.Get("start_time"), q.Get("end_time"))
	v, e := s.GetDeploymentLogs(r.Context(), r.PathValue("id"), id, opts)
	s.deploymentReply(w, v, e)
}
