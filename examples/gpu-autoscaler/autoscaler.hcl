# Nomad Autoscaler Configuration for GPU Management
# This configuration enables the GPU autoscaler plugins for managing
# AI workload resources alongside gaming.

nomad {
  address = "http://localhost:4646"
}

# HTTP API for monitoring (optional)
http {
  bind_address = "127.0.0.1"
  bind_port    = 8080
}

# Policy configuration
policy {
  dir = "./policies"

  # Default values for all policies
  default_evaluation_interval = "5s"
  default_cooldown            = "30s"
}

# GPU Monitor APM Plugin
# Monitors Steam processes and GPU resource usage
apm "gpu-monitor" {
  driver = "gpu-monitor"

  config = {
    # Enable Steam process monitoring
    monitor_steam = "true"

    # Additional game processes to monitor (comma-separated)
    # Add specific game executables here if needed
    game_processes = ""

    # VRAM threshold in MB
    vram_threshold_mb = "4096"
  }
}

# Resource-Aware Strategy Plugin
# Makes intelligent scaling decisions based on GPU availability
strategy "resource-aware" {
  driver = "resource-aware"
}

# AI Model Manager Target Plugin
# Manages model loading/unloading via application APIs
target "ai-model-manager" {
  driver = "ai-model-manager"
}

# Telemetry (optional)
telemetry {
  prometheus_metrics = true
  disable_hostname   = false
}
