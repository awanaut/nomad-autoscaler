// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-autoscaler/plugins"
	gpumonitor "github.com/hashicorp/nomad-autoscaler/plugins/builtin/apm/gpu-monitor/plugin"
)

func main() {
	plugins.Serve(factory)
}

// factory returns a new instance of the GPU Monitor APM plugin.
func factory(log hclog.Logger) interface{} {
	return gpumonitor.NewGPUMonitorPlugin(log)
}
