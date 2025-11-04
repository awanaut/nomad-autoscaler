// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	hclog "github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad-autoscaler/plugins"
	aimodelmanager "github.com/hashicorp/nomad-autoscaler/plugins/builtin/target/ai-model-manager/plugin"
)

func main() {
	plugins.Serve(factory)
}

// factory returns a new instance of the AI Model Manager Target plugin.
func factory(log hclog.Logger) interface{} {
	return aimodelmanager.NewAIModelManagerPlugin(log)
}
