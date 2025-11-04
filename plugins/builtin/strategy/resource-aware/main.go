// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-autoscaler/plugins"
	resourceaware "github.com/hashicorp/nomad-autoscaler/plugins/builtin/strategy/resource-aware/plugin"
)

func main() {
	plugins.Serve(factory)
}

// factory returns a new instance of the Resource-Aware Strategy plugin.
func factory(log hclog.Logger) interface{} {
	return resourceaware.NewResourceAwarePlugin(log)
}
