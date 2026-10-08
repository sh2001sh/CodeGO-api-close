package adminops

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type MemoryStats struct {
	Alloc        uint64 `json:"alloc"`
	TotalAlloc   uint64 `json:"total_alloc"`
	Sys          uint64 `json:"sys"`
	NumGC        uint32 `json:"num_gc"`
	NumGoroutine int    `json:"num_goroutine"`
}
type DiskCacheInfo struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	FileCount int    `json:"file_count"`
	TotalSize int64  `json:"total_size"`
}
type LogFileInfo struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}
type LogCleanupResult struct {
	DeletedCount int      `json:"deleted_count"`
	FreedBytes   int64    `json:"freed_bytes"`
	FailedFiles  []string `json:"failed_files"`
}

func (s *Server) registerPerformance(mux *http.ServeMux, auth Authenticate) {
	routes := map[string]actorHandler{
		"GET /api/performance/stats":         s.performanceStatsHTTP,
		"POST /api/performance/gc":           func(w http.ResponseWriter, _ *http.Request, _ Actor) { runtime.GC(); respond(w, nil) },
		"POST /api/performance/reset_stats":  func(w http.ResponseWriter, _ *http.Request, _ Actor) { s.requests.Store(0); respond(w, nil) },
		"DELETE /api/performance/disk_cache": s.clearDiskCacheHTTP,
		"GET /api/performance/logs":          s.logFilesHTTP,
		"DELETE /api/performance/logs":       s.cleanupLogsHTTP,
	}
	for p, h := range routes {
		mux.HandleFunc(p, s.protected(auth, "root", h))
	}
}
func directoryFiles(dir string, logs bool) ([]LogFileInfo, error) {
	files := make([]LogFileInfo, 0)
	if dir == "" {
		return files, nil
	}
	root, e := filepath.Abs(dir)
	if e != nil {
		return nil, e
	}
	info, e := os.Lstat(root)
	if e != nil {
		return nil, e
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("configured directory is not a regular directory")
	}
	entries, e := os.ReadDir(root)
	if e != nil {
		return nil, e
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		if logs && (!strings.HasSuffix(name, ".log") || !strings.HasPrefix(name, "oneapi-") && !strings.HasPrefix(name, "codego-")) {
			continue
		}
		info, e := entry.Info()
		if e != nil {
			return nil, e
		}
		files = append(files, LogFileInfo{Name: name, Size: info.Size(), ModTime: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].ModTime.Equal(files[j].ModTime) {
			return files[i].Name > files[j].Name
		}
		return files[i].ModTime.After(files[j].ModTime)
	})
	return files, nil
}
func (s *Server) performanceStatsHTTP(w http.ResponseWriter, _ *http.Request, _ Actor) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	cache := DiskCacheInfo{Path: s.cfg.DiskCacheDir}
	if cache.Path != "" {
		files, e := directoryFiles(cache.Path, false)
		if e != nil {
			s.filesError(w, e)
			return
		}
		cache.Exists = true
		cache.FileCount = len(files)
		for _, f := range files {
			cache.TotalSize += f.Size
		}
	}
	stats := map[string]any{"memory_stats": MemoryStats{m.Alloc, m.TotalAlloc, m.Sys, m.NumGC, runtime.NumGoroutine()}, "disk_cache_info": cache, "config": map[string]any{"disk_cache_enabled": cache.Exists, "disk_cache_path": cache.Path, "is_running_in_container": os.Getenv("V3_SERVICE") != "", "monitor_enabled": false}, "cache_stats": map[string]any{}, "requests": s.requests.Load(), "uptime_seconds": int64(s.cfg.Now().Sub(s.started).Seconds()), "service": "control", "log_output": s.logDestination()}
	if s.pool != nil {
		p := s.pool.Stat()
		stats["database_pool"] = map[string]any{"acquired": p.AcquiredConns(), "idle": p.IdleConns(), "total": p.TotalConns(), "max": p.MaxConns()}
	}
	respond(w, stats)
}
func (s *Server) clearDiskCacheHTTP(w http.ResponseWriter, _ *http.Request, _ Actor) {
	if s.cfg.DiskCacheDir == "" {
		fail(w, 409, "cache_disabled", "This v3 service uses memory snapshots and has no filesystem request cache")
		return
	}
	files, e := directoryFiles(s.cfg.DiskCacheDir, false)
	if e != nil {
		s.filesError(w, e)
		return
	}
	old := make([]LogFileInfo, 0)
	cutoff := s.cfg.Now().Add(-10 * time.Minute)
	for _, f := range files {
		if f.ModTime.Before(cutoff) {
			old = append(old, f)
		}
	}
	result, e := removeFiles(s.cfg.DiskCacheDir, old, "")
	if e != nil {
		s.filesError(w, e)
		return
	}
	respond(w, result)
}
func (s *Server) logFilesHTTP(w http.ResponseWriter, _ *http.Request, _ Actor) {
	files, e := directoryFiles(s.cfg.LogDir, true)
	if e != nil {
		s.filesError(w, e)
		return
	}
	var total int64
	for _, f := range files {
		total += f.Size
	}
	data := map[string]any{"log_dir": s.cfg.LogDir, "enabled": s.cfg.LogDir != "", "file_count": len(files), "total_size": total, "files": files, "output": s.logDestination()}
	if len(files) > 0 {
		data["newest_time"] = files[0].ModTime
		data["oldest_time"] = files[len(files)-1].ModTime
	}
	respond(w, data)
}
func (s *Server) cleanupLogsHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	mode := r.URL.Query().Get("mode")
	value, e := strconv.Atoi(r.URL.Query().Get("value"))
	if e != nil || value < 1 || value > 100000 || mode != "by_count" && mode != "by_days" {
		fail(w, 400, "invalid_cleanup", "Expected by_count or by_days and a positive retention value")
		return
	}
	if s.cfg.LogDir == "" {
		fail(w, 409, "file_logs_disabled", "This v3 service logs to standard error; configure a log directory to manage retained files")
		return
	}
	active := ""
	if s.cfg.ActiveLog != nil {
		active = s.cfg.ActiveLog()
		if active == "" {
			fail(w, 503, "active_log_unavailable", "The legacy active log file could not be determined")
			return
		}
	}
	files, e := directoryFiles(s.cfg.LogDir, true)
	if e != nil {
		s.filesError(w, e)
		return
	}
	old := make([]LogFileInfo, 0)
	cutoff := s.cfg.Now().AddDate(0, 0, -value)
	for i, f := range files {
		if mode == "by_count" && i >= value || mode == "by_days" && f.ModTime.Before(cutoff) {
			old = append(old, f)
		}
	}
	result, e := removeFiles(s.cfg.LogDir, old, active)
	if e != nil {
		s.filesError(w, e)
		return
	}
	respond(w, result)
}
func removeFiles(dir string, files []LogFileInfo, active string) (LogCleanupResult, error) {
	result := LogCleanupResult{FailedFiles: make([]string, 0)}
	root, e := filepath.Abs(dir)
	if e != nil {
		return result, e
	}
	activePath := ""
	if active != "" {
		activePath, e = filepath.Abs(active)
		if e != nil {
			return result, e
		}
	}
	for _, f := range files {
		path := filepath.Join(root, f.Name)
		relative, e := filepath.Rel(root, path)
		if e != nil || relative != f.Name || filepath.Base(f.Name) != f.Name {
			return result, errors.New("invalid cleanup path")
		}
		// Native services share a directory and each marks its own active file.
		// Legacy oneapi names have no marker; preserve them without a resolver.
		if path == activePath || strings.HasSuffix(f.Name, ".active.log") || active == "" && strings.HasPrefix(f.Name, "oneapi-") {
			continue
		}
		info, e := os.Lstat(path)
		if errors.Is(e, fs.ErrNotExist) {
			continue
		}
		if e != nil || !info.Mode().IsRegular() {
			result.FailedFiles = append(result.FailedFiles, f.Name)
			continue
		}
		if e = os.Remove(path); e != nil {
			result.FailedFiles = append(result.FailedFiles, f.Name)
			continue
		}
		result.DeletedCount++
		result.FreedBytes += info.Size()
	}
	if len(result.FailedFiles) > 0 {
		return result, errors.New("some retained files could not be removed")
	}
	return result, nil
}
func (s *Server) logDestination() string {
	if s.cfg.LogDir != "" {
		return "stderr,file"
	}
	return "stderr"
}
func (s *Server) filesError(w http.ResponseWriter, err error) {
	s.log.Error("administrative file operation failed", "err", err)
	fail(w, 503, "filesystem_unavailable", "Configured files are unavailable or cleanup failed")
}
