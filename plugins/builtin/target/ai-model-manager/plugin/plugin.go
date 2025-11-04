// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-autoscaler/plugins"
	"github.com/hashicorp/nomad-autoscaler/plugins/base"
	"github.com/hashicorp/nomad-autoscaler/plugins/target"
	"github.com/hashicorp/nomad-autoscaler/sdk"
)

const (
	// pluginName is the unique name of the this plugin amongst target plugins
	pluginName = "ai-model-manager"

	// Configuration keys
	configKeyComfyUIURL          = "comfyui_url"
	configKeyComfyUIEnabled      = "comfyui_enabled"
	configKeySDWebUIURL          = "sdwebui_url"
	configKeySDWebUIEnabled      = "sdwebui_enabled"
	configKeyInvokeAIURL         = "invokeai_url"
	configKeyInvokeAIEnabled     = "invokeai_enabled"
	configKeyInterruptRunning    = "interrupt_running_jobs"
	configKeyWaitTimeout         = "wait_timeout_seconds"

	// Defaults
	defaultComfyUIURL       = "http://localhost:8188"
	defaultSDWebUIURL       = "http://localhost:7860"
	defaultInvokeAIURL      = "http://localhost:9090"
	defaultWaitTimeout      = 30
	defaultInterruptRunning = true
)

var (
	PluginID = plugins.PluginID{
		Name:       pluginName,
		PluginType: sdk.PluginTypeTarget,
	}

	PluginConfig = &plugins.InternalPluginConfig{
		Factory: func(l hclog.Logger) interface{} { return NewAIModelManagerPlugin(l) },
	}

	pluginInfo = &base.PluginInfo{
		Name:       pluginName,
		PluginType: sdk.PluginTypeTarget,
	}
)

// Assert that TargetPlugin meets the target.Target interface
var _ target.Target = (*TargetPlugin)(nil)

// TargetPlugin manages AI model loading/unloading via HTTP APIs
type TargetPlugin struct {
	logger hclog.Logger
	client *http.Client

	// Current state tracking
	modelsLoaded bool
}

// NewAIModelManagerPlugin returns the AI Model Manager implementation of the target.Target interface
func NewAIModelManagerPlugin(log hclog.Logger) target.Target {
	return &TargetPlugin{
		logger: log,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		modelsLoaded: true, // Assume loaded at start
	}
}

// SetConfig satisfies the SetConfig function on the base.Base interface
func (t *TargetPlugin) SetConfig(_ map[string]string) error {
	// Plugin-level config is minimal; most config comes from policy target config
	return nil
}

// PluginInfo satisfies the PluginInfo function on the base.Base interface
func (t *TargetPlugin) PluginInfo() (*base.PluginInfo, error) {
	return pluginInfo, nil
}

// Scale satisfies the Scale function on the target.Target interface
func (t *TargetPlugin) Scale(action sdk.ScalingAction, config map[string]string) error {
	t.logger.Info("AI model scaling action",
		"count", action.Count,
		"direction", action.Direction,
		"reason", action.Reason)

	// Parse configuration
	cfg, err := parseConfig(config)
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Determine action: unload (count=0) or load (count=1)
	if action.Count == 0 {
		return t.unloadModels(cfg)
	} else if action.Count == 1 {
		return t.loadModels(cfg)
	}

	return fmt.Errorf("invalid count value: %d (expected 0 or 1)", action.Count)
}

// Status satisfies the Status function on the target.Target interface
func (t *TargetPlugin) Status(config map[string]string) (*sdk.TargetStatus, error) {
	cfg, err := parseConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Check if any enabled AI tools are healthy
	ready := false
	meta := make(map[string]string)

	if cfg.comfyuiEnabled {
		healthy, queueStatus := t.checkComfyUIStatus(cfg.comfyuiURL)
		meta["comfyui_healthy"] = strconv.FormatBool(healthy)
		meta["comfyui_queue"] = queueStatus
		if healthy {
			ready = true
		}
	}

	if cfg.sdwebuiEnabled {
		healthy := t.checkSDWebUIStatus(cfg.sdwebuiURL)
		meta["sdwebui_healthy"] = strconv.FormatBool(healthy)
		if healthy {
			ready = true
		}
	}

	if cfg.invokeaiEnabled {
		healthy := t.checkInvokeAIStatus(cfg.invokeaiURL)
		meta["invokeai_healthy"] = strconv.FormatBool(healthy)
		if healthy {
			ready = true
		}
	}

	// Count represents current state: 0 = unloaded, 1 = loaded
	count := int64(0)
	if t.modelsLoaded {
		count = 1
	}

	status := &sdk.TargetStatus{
		Ready: ready,
		Count: count,
		Meta:  meta,
	}

	t.logger.Debug("target status checked",
		"ready", ready,
		"count", count,
		"models_loaded", t.modelsLoaded)

	return status, nil
}

// unloadModels unloads models from all enabled AI applications
func (t *TargetPlugin) unloadModels(cfg *targetConfig) error {
	t.logger.Info("unloading AI models from VRAM")

	errors := []error{}

	// Unload from ComfyUI
	if cfg.comfyuiEnabled {
		if err := t.unloadComfyUI(cfg); err != nil {
			t.logger.Error("failed to unload ComfyUI models", "error", err)
			errors = append(errors, fmt.Errorf("ComfyUI: %w", err))
		} else {
			t.logger.Info("ComfyUI models unloaded successfully")
		}
	}

	// Unload from SD WebUI
	if cfg.sdwebuiEnabled {
		if err := t.unloadSDWebUI(cfg); err != nil {
			t.logger.Error("failed to unload SD WebUI models", "error", err)
			errors = append(errors, fmt.Errorf("SD WebUI: %w", err))
		} else {
			t.logger.Info("SD WebUI models unloaded successfully")
		}
	}

	// Unload from InvokeAI
	if cfg.invokeaiEnabled {
		if err := t.unloadInvokeAI(cfg); err != nil {
			t.logger.Error("failed to unload InvokeAI models", "error", err)
			errors = append(errors, fmt.Errorf("InvokeAI: %w", err))
		} else {
			t.logger.Info("InvokeAI models unloaded successfully")
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to unload models from %d application(s)", len(errors))
	}

	t.modelsLoaded = false
	return nil
}

// loadModels is a no-op since models auto-load on first request
func (t *TargetPlugin) loadModels(cfg *targetConfig) error {
	t.logger.Info("marking models as available for loading")
	// Models will be loaded automatically when the applications receive requests
	// This is just a state marker
	t.modelsLoaded = true
	return nil
}

// unloadComfyUI unloads models from ComfyUI
func (t *TargetPlugin) unloadComfyUI(cfg *targetConfig) error {
	// First, interrupt any running jobs
	if cfg.interruptRunning {
		req, err := http.NewRequest("POST", cfg.comfyuiURL+"/interrupt", nil)
		if err != nil {
			return fmt.Errorf("failed to create interrupt request: %w", err)
		}

		resp, err := t.client.Do(req)
		if err != nil {
			t.logger.Warn("failed to interrupt ComfyUI jobs", "error", err)
		} else {
			resp.Body.Close()
			t.logger.Debug("interrupted ComfyUI jobs")
		}

		// Wait a moment for jobs to stop
		time.Sleep(1 * time.Second)
	}

	// Now unload models
	payload := map[string]interface{}{
		"unload_models": true,
		"free_memory":   true,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", cfg.comfyuiURL+"/free", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// unloadSDWebUI unloads models from Stable Diffusion WebUI
func (t *TargetPlugin) unloadSDWebUI(cfg *targetConfig) error {
	// SD WebUI uses the unload-checkpoint endpoint
	req, err := http.NewRequest("POST", cfg.sdwebuiURL+"/sdapi/v1/unload-checkpoint", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// unloadInvokeAI unloads models from InvokeAI
func (t *TargetPlugin) unloadInvokeAI(cfg *targetConfig) error {
	// InvokeAI may not have a direct unload endpoint
	// This is a placeholder - adjust based on actual InvokeAI API
	t.logger.Warn("InvokeAI model unloading not fully implemented - may require custom logic")
	return nil
}

// checkComfyUIStatus checks if ComfyUI is healthy and returns queue status
func (t *TargetPlugin) checkComfyUIStatus(url string) (bool, string) {
	resp, err := t.client.Get(url + "/queue")
	if err != nil {
		t.logger.Debug("ComfyUI health check failed", "error", err)
		return false, "unavailable"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, "unhealthy"
	}

	// Parse queue status
	var queueData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&queueData); err != nil {
		return true, "healthy"
	}

	running := 0
	pending := 0

	if qr, ok := queueData["queue_running"].([]interface{}); ok {
		running = len(qr)
	}
	if qp, ok := queueData["queue_pending"].([]interface{}); ok {
		pending = len(qp)
	}

	status := fmt.Sprintf("running:%d,pending:%d", running, pending)
	return true, status
}

// checkSDWebUIStatus checks if SD WebUI is healthy
func (t *TargetPlugin) checkSDWebUIStatus(url string) bool {
	resp, err := t.client.Get(url + "/sdapi/v1/options")
	if err != nil {
		t.logger.Debug("SD WebUI health check failed", "error", err)
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// checkInvokeAIStatus checks if InvokeAI is healthy
func (t *TargetPlugin) checkInvokeAIStatus(url string) bool {
	resp, err := t.client.Get(url + "/api/v1/status")
	if err != nil {
		t.logger.Debug("InvokeAI health check failed", "error", err)
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// targetConfig holds the parsed target configuration
type targetConfig struct {
	comfyuiURL       string
	comfyuiEnabled   bool
	sdwebuiURL       string
	sdwebuiEnabled   bool
	invokeaiURL      string
	invokeaiEnabled  bool
	interruptRunning bool
	waitTimeout      int
}

// parseConfig parses and validates the target configuration
func parseConfig(config map[string]string) (*targetConfig, error) {
	cfg := &targetConfig{
		comfyuiURL:       defaultComfyUIURL,
		comfyuiEnabled:   false,
		sdwebuiURL:       defaultSDWebUIURL,
		sdwebuiEnabled:   false,
		invokeaiURL:      defaultInvokeAIURL,
		invokeaiEnabled:  false,
		interruptRunning: defaultInterruptRunning,
		waitTimeout:      defaultWaitTimeout,
	}

	// ComfyUI configuration
	if url, ok := config[configKeyComfyUIURL]; ok && url != "" {
		cfg.comfyuiURL = url
	}
	if enabled, ok := config[configKeyComfyUIEnabled]; ok && enabled != "" {
		val, err := strconv.ParseBool(enabled)
		if err != nil {
			return nil, fmt.Errorf("invalid value for %q: %v", configKeyComfyUIEnabled, enabled)
		}
		cfg.comfyuiEnabled = val
	}

	// SD WebUI configuration
	if url, ok := config[configKeySDWebUIURL]; ok && url != "" {
		cfg.sdwebuiURL = url
	}
	if enabled, ok := config[configKeySDWebUIEnabled]; ok && enabled != "" {
		val, err := strconv.ParseBool(enabled)
		if err != nil {
			return nil, fmt.Errorf("invalid value for %q: %v", configKeySDWebUIEnabled, enabled)
		}
		cfg.sdwebuiEnabled = val
	}

	// InvokeAI configuration
	if url, ok := config[configKeyInvokeAIURL]; ok && url != "" {
		cfg.invokeaiURL = url
	}
	if enabled, ok := config[configKeyInvokeAIEnabled]; ok && enabled != "" {
		val, err := strconv.ParseBool(enabled)
		if err != nil {
			return nil, fmt.Errorf("invalid value for %q: %v", configKeyInvokeAIEnabled, enabled)
		}
		cfg.invokeaiEnabled = val
	}

	// Interrupt running jobs configuration
	if interrupt, ok := config[configKeyInterruptRunning]; ok && interrupt != "" {
		val, err := strconv.ParseBool(interrupt)
		if err != nil {
			return nil, fmt.Errorf("invalid value for %q: %v", configKeyInterruptRunning, interrupt)
		}
		cfg.interruptRunning = val
	}

	// Wait timeout configuration
	if timeout, ok := config[configKeyWaitTimeout]; ok && timeout != "" {
		val, err := strconv.Atoi(timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid value for %q: %v", configKeyWaitTimeout, timeout)
		}
		cfg.waitTimeout = val
	}

	// Validate at least one tool is enabled
	if !cfg.comfyuiEnabled && !cfg.sdwebuiEnabled && !cfg.invokeaiEnabled {
		return nil, fmt.Errorf("at least one AI tool must be enabled")
	}

	return cfg, nil
}
