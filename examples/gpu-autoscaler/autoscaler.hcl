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
# Monitors GPU processes and Steam activity
apm "gpu-monitor" {
  driver = "gpu-monitor"

  config = {
    # GPU Process Monitoring (RECOMMENDED - Most Accurate)
    # Monitors which processes are actually using the GPU via nvidia-smi
    use_gpu_processes = "true"

    # Option 1: Whitelist File (RECOMMENDED - Easier to maintain)
    # Path to file containing whitelisted AI processes
    # See gpu-whitelist.txt for example format
    whitelist_file = "./examples/gpu-autoscaler/gpu-whitelist.txt"

    # Option 2: Inline Whitelist (Alternative - comma-separated)
    # Uncomment if not using whitelist_file
    # whitelisted_gpu_processes = "python,python3,comfyui,stable-diffusion,ollama"

    # Note: Both options can be used together - they will be merged

    # Minimum VRAM usage (MB) to consider a process as GPU-intensive
    # Processes using less than this are ignored
    gpu_process_memory_threshold_mb = "1024"

    # Steam Process Tree Monitoring (Fallback)
    # Monitors Steam child processes as a heuristic
    monitor_steam = "true"

    # Specific Game Process Names (Optional)
    # Add specific game executables here if GPU process monitoring isn't enough
    game_processes = ""

    # VRAM threshold in MB (for VRAM queries)
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
