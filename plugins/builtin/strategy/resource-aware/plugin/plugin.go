// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package plugin

import (
	"fmt"
	"strconv"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-autoscaler/plugins"
	"github.com/hashicorp/nomad-autoscaler/plugins/base"
	"github.com/hashicorp/nomad-autoscaler/plugins/strategy"
	"github.com/hashicorp/nomad-autoscaler/sdk"
)

const (
	// pluginName is the unique name of the this plugin amongst strategy plugins
	pluginName = "resource-aware"

	// Configuration keys
	runConfigKeyVRAMThreshold    = "vram_threshold_mb"
	runConfigKeyVRAMCoexist      = "vram_coexist_mb"
	runConfigKeyRAMThreshold     = "ram_threshold_mb"
	runConfigKeyAllowCoexistence = "allow_coexistence"
	runConfigKeyMonitorSteam     = "monitor_steam"

	// Defaults
	defaultVRAMThreshold  = 4096  // 4GB
	defaultVRAMCoexist    = 8192  // 8GB
	defaultRAMThreshold   = 8192  // 8GB
	defaultAllowCoexist   = false
)

var (
	PluginID = plugins.PluginID{
		Name:       pluginName,
		PluginType: sdk.PluginTypeStrategy,
	}

	PluginConfig = &plugins.InternalPluginConfig{
		Factory: func(l hclog.Logger) interface{} { return NewResourceAwarePlugin(l) },
	}

	pluginInfo = &base.PluginInfo{
		Name:       pluginName,
		PluginType: sdk.PluginTypeStrategy,
	}
)

// resourceAwareConfig holds the parsed configuration for a strategy run
type resourceAwareConfig struct {
	vramThreshold    float64
	vramCoexist      float64
	ramThreshold     float64
	allowCoexistence bool
	monitorSteam     bool
}

// Assert that StrategyPlugin meets the strategy.Strategy interface
var _ strategy.Strategy = (*StrategyPlugin)(nil)

// StrategyPlugin is the Resource-Aware implementation of the strategy.Strategy interface
type StrategyPlugin struct {
	logger hclog.Logger
}

// NewResourceAwarePlugin returns the Resource-Aware implementation of the strategy.Strategy interface
func NewResourceAwarePlugin(log hclog.Logger) strategy.Strategy {
	return &StrategyPlugin{
		logger: log,
	}
}

// SetConfig satisfies the SetConfig function on the base.Base interface
func (s *StrategyPlugin) SetConfig(_ map[string]string) error {
	return nil
}

// PluginInfo satisfies the PluginInfo function on the base.Base interface
func (s *StrategyPlugin) PluginInfo() (*base.PluginInfo, error) {
	return pluginInfo, nil
}

// Run satisfies the Run function on the strategy.Strategy interface
func (s *StrategyPlugin) Run(eval *sdk.ScalingCheckEvaluation, count int64) (*sdk.ScalingCheckEvaluation, error) {
	if len(eval.Metrics) == 0 {
		s.logger.Trace("no metrics available")
		return eval, nil
	}

	// Parse check config
	config, err := parseConfig(eval.Check.Strategy.Config)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(
		"check_name", eval.Check.Name,
		"current_count", count,
		"vram_threshold", config.vramThreshold,
		"allow_coexistence", config.allowCoexistence,
	)

	// Get the latest metric value (game_running from gpu-monitor APM)
	latestMetric := eval.Metrics[len(eval.Metrics)-1]
	gameRunning := latestMetric.Value > 0.5 // Treat as boolean

	logger.Debug("evaluating GPU resource awareness",
		"game_running", gameRunning,
		"metric_value", latestMetric.Value,
		"current_state", count)

	// Determine desired state
	var newCount int64
	var reason string
	var direction sdk.ScaleDirection

	if gameRunning {
		// Game is running - should we unload models?
		if count == 1 {
			// Models are currently loaded - unload them
			newCount = 0
			direction = sdk.ScaleDirectionDown
			reason = "game detected, unloading AI models to free GPU resources"
			logger.Info("scaling down: game detected")
		} else {
			// Models already unloaded - keep them unloaded
			newCount = 0
			direction = sdk.ScaleDirectionNone
			reason = "game running, models remain unloaded"
			logger.Debug("no action: game running, models already unloaded")
		}
	} else {
		// No game running - should we load models?
		if count == 0 {
			// Models are currently unloaded - load them
			newCount = 1
			direction = sdk.ScaleDirectionUp
			reason = "no game detected, loading AI models"
			logger.Info("scaling up: no game detected")
		} else {
			// Models already loaded - keep them loaded
			newCount = 1
			direction = sdk.ScaleDirectionNone
			reason = "no game running, models remain loaded"
			logger.Debug("no action: no game, models already loaded")
		}
	}

	// Set the action
	eval.Action.Count = newCount
	eval.Action.Direction = direction
	eval.Action.Reason = reason

	logger.Debug("scaling decision made",
		"current_count", count,
		"new_count", newCount,
		"direction", direction,
		"reason", reason)

	return eval, nil
}

// parseConfig parses and validates the policy check config
func parseConfig(config map[string]string) (*resourceAwareConfig, error) {
	c := &resourceAwareConfig{
		vramThreshold:    defaultVRAMThreshold,
		vramCoexist:      defaultVRAMCoexist,
		ramThreshold:     defaultRAMThreshold,
		allowCoexistence: defaultAllowCoexist,
		monitorSteam:     true,
	}

	// Parse VRAM threshold
	if val, ok := config[runConfigKeyVRAMThreshold]; ok && val != "" {
		threshold, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid value for %q: %v", runConfigKeyVRAMThreshold, val)
		}
		c.vramThreshold = threshold
	}

	// Parse VRAM coexistence threshold
	if val, ok := config[runConfigKeyVRAMCoexist]; ok && val != "" {
		coexist, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid value for %q: %v", runConfigKeyVRAMCoexist, val)
		}
		c.vramCoexist = coexist
	}

	// Parse RAM threshold
	if val, ok := config[runConfigKeyRAMThreshold]; ok && val != "" {
		threshold, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid value for %q: %v", runConfigKeyRAMThreshold, val)
		}
		c.ramThreshold = threshold
	}

	// Parse allow_coexistence
	if val, ok := config[runConfigKeyAllowCoexistence]; ok && val != "" {
		allow, err := strconv.ParseBool(val)
		if err != nil {
			return nil, fmt.Errorf("invalid value for %q: %v", runConfigKeyAllowCoexistence, val)
		}
		c.allowCoexistence = allow
	}

	// Parse monitor_steam
	if val, ok := config[runConfigKeyMonitorSteam]; ok && val != "" {
		monitor, err := strconv.ParseBool(val)
		if err != nil {
			return nil, fmt.Errorf("invalid value for %q: %v", runConfigKeyMonitorSteam, val)
		}
		c.monitorSteam = monitor
	}

	return c, nil
}
