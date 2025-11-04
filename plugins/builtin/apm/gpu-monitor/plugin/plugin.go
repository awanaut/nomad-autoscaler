// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package plugin

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-autoscaler/plugins"
	"github.com/hashicorp/nomad-autoscaler/plugins/apm"
	"github.com/hashicorp/nomad-autoscaler/plugins/base"
	"github.com/hashicorp/nomad-autoscaler/sdk"
	"github.com/shirou/gopsutil/v3/process"
)

const (
	// pluginName is the name of the plugin
	pluginName = "gpu-monitor"

	// Configuration keys
	configKeyMonitorSteam       = "monitor_steam"
	configKeyGameProcesses      = "game_processes"
	configKeyVRAMThreshold      = "vram_threshold_mb"
	configKeyQueryMode          = "query_mode"
	configKeyUseGPUProcesses    = "use_gpu_processes"
	configKeyWhitelistedProcs   = "whitelisted_gpu_processes"
	configKeyGPUMemThreshold    = "gpu_process_memory_threshold_mb"
)

var (
	PluginID = plugins.PluginID{
		Name:       pluginName,
		PluginType: sdk.PluginTypeAPM,
	}

	PluginConfig = &plugins.InternalPluginConfig{
		Factory: func(l hclog.Logger) interface{} { return NewGPUMonitorPlugin(l) },
	}

	pluginInfo = &base.PluginInfo{
		Name:       pluginName,
		PluginType: sdk.PluginTypeAPM,
	}
)

// APMPlugin monitors GPU resources and Steam game processes
type APMPlugin struct {
	logger hclog.Logger
	config map[string]string

	// Configuration
	monitorSteam        bool
	gameProcesses       []string
	vramThreshold       int64
	useGPUProcesses     bool
	whitelistedProcs    []string
	gpuMemThreshold     int64

	// Steam process tracking
	steamPID       int32
	lastGameCount  int
}

// NewGPUMonitorPlugin returns a new instance of the GPU Monitor APM plugin
func NewGPUMonitorPlugin(log hclog.Logger) apm.APM {
	return &APMPlugin{
		logger:          log,
		monitorSteam:    true,
		vramThreshold:   4096, // Default 4GB
		useGPUProcesses: true,  // Default to GPU process monitoring
		gpuMemThreshold: 1024,  // Default 1GB threshold
		whitelistedProcs: []string{
			"python", "python3", "comfyui", "stable-diffusion", "ollama",
		},
	}
}

// SetConfig satisfies the SetConfig function on the base.Base interface
func (a *APMPlugin) SetConfig(config map[string]string) error {
	a.config = config

	// Parse monitor_steam config
	if val, ok := config[configKeyMonitorSteam]; ok {
		monitorSteam, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid value for %q: %v", configKeyMonitorSteam, val)
		}
		a.monitorSteam = monitorSteam
	}

	// Parse game_processes config (comma-separated list)
	if val, ok := config[configKeyGameProcesses]; ok && val != "" {
		a.gameProcesses = strings.Split(val, ",")
		for i := range a.gameProcesses {
			a.gameProcesses[i] = strings.TrimSpace(a.gameProcesses[i])
		}
	}

	// Parse VRAM threshold
	if val, ok := config[configKeyVRAMThreshold]; ok {
		threshold, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid value for %q: %v", configKeyVRAMThreshold, val)
		}
		a.vramThreshold = threshold
	}

	// Parse use_gpu_processes config
	if val, ok := config[configKeyUseGPUProcesses]; ok {
		useGPU, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("invalid value for %q: %v", configKeyUseGPUProcesses, val)
		}
		a.useGPUProcesses = useGPU
	}

	// Parse whitelisted GPU processes
	if val, ok := config[configKeyWhitelistedProcs]; ok && val != "" {
		a.whitelistedProcs = strings.Split(val, ",")
		for i := range a.whitelistedProcs {
			a.whitelistedProcs[i] = strings.TrimSpace(a.whitelistedProcs[i])
		}
	}

	// Parse GPU memory threshold
	if val, ok := config[configKeyGPUMemThreshold]; ok {
		threshold, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid value for %q: %v", configKeyGPUMemThreshold, val)
		}
		a.gpuMemThreshold = threshold
	}

	a.logger.Info("GPU Monitor APM configured",
		"monitor_steam", a.monitorSteam,
		"use_gpu_processes", a.useGPUProcesses,
		"gpu_mem_threshold_mb", a.gpuMemThreshold,
		"game_processes", a.gameProcesses,
		"whitelisted_procs", a.whitelistedProcs,
		"vram_threshold_mb", a.vramThreshold)

	return nil
}

// PluginInfo satisfies the PluginInfo function on the base.Base interface
func (a *APMPlugin) PluginInfo() (*base.PluginInfo, error) {
	return pluginInfo, nil
}

// Query returns metrics for the given query
func (a *APMPlugin) Query(q string, r sdk.TimeRange) (sdk.TimestampedMetrics, error) {
	m, err := a.QueryMultiple(q, r)
	if err != nil {
		return nil, err
	}

	if len(m) == 0 {
		return sdk.TimestampedMetrics{}, nil
	}

	return m[0], nil
}

// QueryMultiple returns multiple metric streams
func (a *APMPlugin) QueryMultiple(q string, r sdk.TimeRange) ([]sdk.TimestampedMetrics, error) {
	a.logger.Debug("querying GPU metrics", "query", q, "range", r)

	var metrics sdk.TimestampedMetrics
	now := time.Now()

	// Determine what to query based on the query string
	switch q {
	case "game_running":
		value := a.checkGameRunning()
		metrics = append(metrics, sdk.TimestampedMetric{
			Timestamp: now,
			Value:     value,
		})

	case "vram_free":
		value, err := a.queryVRAMFree()
		if err != nil {
			return nil, fmt.Errorf("failed to query VRAM: %v", err)
		}
		metrics = append(metrics, sdk.TimestampedMetric{
			Timestamp: now,
			Value:     value,
		})

	case "gpu_utilization":
		value, err := a.queryGPUUtilization()
		if err != nil {
			return nil, fmt.Errorf("failed to query GPU utilization: %v", err)
		}
		metrics = append(metrics, sdk.TimestampedMetric{
			Timestamp: now,
			Value:     value,
		})

	case "system":
		// Return multiple metrics
		gameRunning := a.checkGameRunning()
		vramFree, err := a.queryVRAMFree()
		if err != nil {
			return nil, fmt.Errorf("failed to query VRAM: %v", err)
		}

		// For "system" query, we return game_running as the primary metric
		// The strategy plugin will need to handle this
		metrics = append(metrics, sdk.TimestampedMetric{
			Timestamp: now,
			Value:     gameRunning,
		})

		// Log additional context
		a.logger.Debug("system metrics collected",
			"game_running", gameRunning,
			"vram_free_mb", vramFree)

	default:
		return nil, fmt.Errorf("unsupported query: %s", q)
	}

	return []sdk.TimestampedMetrics{metrics}, nil
}

// checkGameRunning checks if any game processes are running
// Returns 1.0 if game detected, 0.0 otherwise
func (a *APMPlugin) checkGameRunning() float64 {
	// Priority 1: Check GPU processes (most accurate)
	if a.useGPUProcesses {
		if a.checkGPUProcesses() {
			return 1.0
		}
	}

	// Priority 2: Check specific process names
	if len(a.gameProcesses) > 0 {
		if a.checkSpecificProcesses() {
			return 1.0
		}
	}

	// Priority 3: Steam process count heuristic (fallback)
	if a.monitorSteam {
		if a.checkSteamGames() {
			return 1.0
		}
	}

	return 0.0
}

// checkGPUProcesses checks if any non-whitelisted processes are using the GPU
// This is the most accurate method for game detection
func (a *APMPlugin) checkGPUProcesses() bool {
	cmd := exec.Command("nvidia-smi",
		"--query-compute-apps=pid,process_name,used_memory",
		"--format=csv,noheader,nounits")

	output, err := cmd.Output()
	if err != nil {
		a.logger.Debug("failed to query GPU processes", "error", err)
		return false
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse CSV: pid, process_name, used_memory
		parts := strings.Split(line, ", ")
		if len(parts) < 3 {
			continue
		}

		processName := strings.TrimSpace(parts[1])
		usedMemStr := strings.TrimSpace(parts[2])

		// Parse memory usage
		usedMem, err := strconv.ParseInt(usedMemStr, 10, 64)
		if err != nil {
			a.logger.Debug("failed to parse GPU memory", "value", usedMemStr)
			continue
		}

		// Check if this process exceeds threshold
		if usedMem < a.gpuMemThreshold {
			continue
		}

		// Check if process is whitelisted (AI apps)
		isWhitelisted := false
		processLower := strings.ToLower(processName)
		for _, whitelisted := range a.whitelistedProcs {
			if strings.Contains(processLower, strings.ToLower(whitelisted)) {
				isWhitelisted = true
				break
			}
		}

		if !isWhitelisted {
			a.logger.Info("non-whitelisted GPU process detected",
				"process", processName,
				"vram_mb", usedMem)
			return true
		}
	}

	return false
}

// checkSteamGames checks if Steam is running games
func (a *APMPlugin) checkSteamGames() bool {
	// Find Steam process if not already found
	if a.steamPID == 0 {
		procs, err := process.Processes()
		if err != nil {
			a.logger.Warn("failed to enumerate processes", "error", err)
			return false
		}

		for _, proc := range procs {
			name, err := proc.Name()
			if err != nil {
				continue
			}

			if strings.Contains(strings.ToLower(name), "steam") {
				a.steamPID = proc.Pid
				a.logger.Debug("found Steam process", "pid", a.steamPID)
				break
			}
		}
	}

	// Check if Steam is still running
	if a.steamPID != 0 {
		proc, err := process.NewProcess(a.steamPID)
		if err != nil {
			a.logger.Debug("Steam process no longer exists", "pid", a.steamPID)
			a.steamPID = 0
			a.lastGameCount = 0
			return false
		}

		// Get child processes
		children, err := proc.Children()
		if err != nil {
			a.logger.Debug("failed to get Steam children", "error", err)
			return false
		}

		// Steam usually has ~3-5 base processes
		// Games add significant additional processes
		// This is a heuristic - tune based on your system
		gameCount := len(children)
		if gameCount > 5 {
			if gameCount != a.lastGameCount {
				a.logger.Info("Steam game activity detected",
					"child_processes", gameCount,
					"previous_count", a.lastGameCount)
				a.lastGameCount = gameCount
			}
			return true
		}

		if a.lastGameCount > 5 && gameCount <= 5 {
			a.logger.Info("Steam game activity ended",
				"child_processes", gameCount)
		}
		a.lastGameCount = gameCount
	}

	return false
}

// checkSpecificProcesses checks if any configured game processes are running
func (a *APMPlugin) checkSpecificProcesses() bool {
	procs, err := process.Processes()
	if err != nil {
		a.logger.Warn("failed to enumerate processes", "error", err)
		return false
	}

	for _, proc := range procs {
		name, err := proc.Name()
		if err != nil {
			continue
		}

		nameLower := strings.ToLower(name)
		for _, gameProc := range a.gameProcesses {
			if strings.Contains(nameLower, strings.ToLower(gameProc)) {
				a.logger.Info("game process detected", "process", name)
				return true
			}
		}
	}

	return false
}

// queryVRAMFree queries free VRAM in MB using nvidia-smi
func (a *APMPlugin) queryVRAMFree() (float64, error) {
	cmd := exec.Command("nvidia-smi",
		"--query-gpu=memory.free",
		"--format=csv,noheader,nounits")

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("nvidia-smi failed: %v", err)
	}

	freeMBStr := strings.TrimSpace(string(output))
	freeMB, err := strconv.ParseFloat(freeMBStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse VRAM value %q: %v", freeMBStr, err)
	}

	return freeMB, nil
}

// queryGPUUtilization queries GPU utilization percentage using nvidia-smi
func (a *APMPlugin) queryGPUUtilization() (float64, error) {
	cmd := exec.Command("nvidia-smi",
		"--query-gpu=utilization.gpu",
		"--format=csv,noheader,nounits")

	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("nvidia-smi failed: %v", err)
	}

	utilStr := strings.TrimSpace(string(output))
	util, err := strconv.ParseFloat(utilStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse GPU utilization %q: %v", utilStr, err)
	}

	return util, nil
}
