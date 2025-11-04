# GPU Autoscaler Plugin for Gaming + AI Workloads

This plugin suite enables intelligent GPU resource management for systems running both gaming and AI workloads (like ComfyUI, Stable Diffusion WebUI, etc.) on a single machine.

## Overview

The GPU autoscaler automatically unloads AI models from VRAM when you launch games, and reloads them when you're done gaming. This is achieved through three custom plugins:

1. **GPU Monitor (APM Plugin)**: Monitors Steam processes and GPU resources
2. **Resource-Aware (Strategy Plugin)**: Makes intelligent scaling decisions based on GPU availability
3. **AI Model Manager (Target Plugin)**: Calls AI application APIs to unload/load models

## Key Features

- **Fast Response**: Uses API calls instead of container restarts (~3s vs ~30s+)
- **Graceful Model Management**: Keeps applications running, just unloads models from VRAM
- **Steam Integration**: Automatically detects when Steam launches games
- **Multi-App Support**: Works with ComfyUI, SD WebUI, InvokeAI, and more
- **Configurable Thresholds**: Fine-tune behavior based on your system resources
- **Coexistence Mode**: Optionally allow AI workloads during light gaming

## Architecture

```
Steam Game Launch → GPU Monitor (APM) → Resource-Aware (Strategy) → AI Model Manager (Target)
                         ↓                       ↓                          ↓
                  Detects processes      Decides to scale down      Calls ComfyUI API
                  Queries VRAM usage     Based on resources         to unload models
```

## Prerequisites

- Linux system (Windows support coming later)
- NVIDIA GPU with nvidia-smi available
- ComfyUI or other AI generation tools running
- Steam installed (optional, can also monitor specific processes)
- Nomad and Nomad Autoscaler installed

## Installation

### 1. Build the Autoscaler with Custom Plugins

The plugins are built into the Nomad Autoscaler binary:

```bash
cd nomad-autoscaler
make build
```

This will create a `nomad-autoscaler` binary with the GPU plugins included.

### 2. Configure the Autoscaler

Create an autoscaler configuration file `autoscaler.hcl`:

```hcl
nomad {
  address = "http://localhost:4646"
}

policy {
  dir = "/etc/nomad-autoscaler/policies"
  default_evaluation_interval = "5s"
  default_cooldown            = "30s"
}

apm "gpu-monitor" {
  driver = "gpu-monitor"

  config = {
    monitor_steam      = "true"
    game_processes     = "steam,game.exe"  # Additional processes to monitor
    vram_threshold_mb  = "4096"
  }
}

strategy "resource-aware" {
  driver = "resource-aware"
}

target "ai-model-manager" {
  driver = "ai-model-manager"
}
```

### 3. Create a GPU Policy

Create `/etc/nomad-autoscaler/policies/gpu-management.hcl`:

```hcl
scaling "comfyui-gpu-manager" {
    enabled = true
    min     = 0    # 0 = models unloaded
    max     = 1    # 1 = models loaded
    type    = "horizontal"

    policy {
        evaluation_interval = "3s"    # Check every 3 seconds
        cooldown            = "10s"   # Wait 10s after unloading
        cooldown_on_scale_up = "30s"  # Wait 30s after loading

        check "gpu_resources" {
            source   = "gpu-monitor"
            query    = "system"       # Query local system

            strategy "resource-aware" {
                # Steam game detection
                monitor_steam = "true"

                # Resource thresholds
                vram_threshold_mb = "4096"      # Unload if less than 4GB free
                vram_coexist_mb = "8192"        # Allow coexistence with 8GB+ free
                ram_threshold_mb = "8192"       # Also check system RAM

                # Allow AI workloads to run during light gaming
                allow_coexistence = "false"
            }
        }

        target "ai-model-manager" {
            # ComfyUI configuration
            comfyui_url = "http://localhost:8188"
            comfyui_enabled = "true"

            # SD WebUI configuration (if running)
            sdwebui_url = "http://localhost:7860"
            sdwebui_enabled = "false"

            # InvokeAI configuration (if running)
            invokeai_url = "http://localhost:9090"
            invokeai_enabled = "false"

            # Behavior
            interrupt_running_jobs = "true"
            wait_timeout_seconds = "30"
        }
    }
}
```

### 4. Run the Autoscaler

```bash
./nomad-autoscaler agent -config=autoscaler.hcl
```

## Configuration Reference

### GPU Monitor APM Plugin

**Config Options:**

- `use_gpu_processes` (bool, default: true): **RECOMMENDED** - Monitor actual GPU process list (most accurate)
- `whitelisted_gpu_processes` (string, comma-separated, default: "python,python3,comfyui,stable-diffusion,ollama"): AI processes to ignore
- `gpu_process_memory_threshold_mb` (int, default: 1024): Minimum VRAM usage to consider a process as GPU-intensive
- `monitor_steam` (bool, default: true): Enable Steam process tree monitoring (fallback method)
- `game_processes` (string, comma-separated): Additional game processes to monitor by name (e.g., "game.exe,RocketLeague.exe")
- `vram_threshold_mb` (int, default: 4096): VRAM threshold in MB

**Detection Methods (Priority Order):**

1. **GPU Process Monitoring** (Primary - Most Accurate):
   - Queries `nvidia-smi --query-compute-apps` to see which processes are actively using the GPU
   - Any non-whitelisted process using >1GB VRAM → Game detected
   - Works with ANY game launcher (Steam, Epic, GOG, standalone)
   - No false positives from background tasks

2. **Specific Process Names** (Secondary):
   - Scans all running processes for configured game names
   - Accurate but requires manual configuration per game

3. **Steam Process Tree** (Fallback):
   - Counts Steam child processes (heuristic: >5 processes = game running)
   - Simple but less accurate (false positives possible)

**Query Modes:**

- `system`: Returns game running status (1.0 if game detected, 0.0 otherwise)
- `game_running`: Same as system
- `vram_free`: Returns free VRAM in MB
- `gpu_utilization`: Returns GPU utilization percentage

### Resource-Aware Strategy Plugin

**Config Options:**

- `vram_threshold_mb` (int, default: 4096): Minimum free VRAM before unloading models
- `vram_coexist_mb` (int, default: 8192): VRAM threshold for coexistence mode
- `ram_threshold_mb` (int, default: 8192): System RAM threshold
- `allow_coexistence` (bool, default: false): Allow AI models to stay loaded if enough resources
- `monitor_steam` (bool, default: true): Use Steam monitoring

**Scaling Logic:**

- If game detected: scale down (count = 0, unload models)
- If no game and models unloaded: scale up (count = 1, load models)
- If coexistence allowed and resources available: keep models loaded

### AI Model Manager Target Plugin

**Config Options:**

- `comfyui_url` (string, default: "http://localhost:8188"): ComfyUI API URL
- `comfyui_enabled` (bool, default: false): Enable ComfyUI management
- `sdwebui_url` (string, default: "http://localhost:7860"): SD WebUI API URL
- `sdwebui_enabled` (bool, default: false): Enable SD WebUI management
- `invokeai_url` (string, default: "http://localhost:9090"): InvokeAI API URL
- `invokeai_enabled` (bool, default: false): Enable InvokeAI management
- `interrupt_running_jobs` (bool, default: true): Interrupt running inference jobs before unloading
- `wait_timeout_seconds` (int, default: 30): Timeout for waiting for jobs to complete

**How It Works:**

When scaling down (count = 0):
1. Interrupts any running inference jobs (if enabled)
2. Calls each enabled AI app's unload API
3. Models are moved from VRAM to system RAM or unloaded entirely
4. Applications remain running for fast recovery

When scaling up (count = 1):
- Models are marked as available
- They auto-load when the next inference request comes in

## Workflow Example

### 1. Normal State (No Game)

```
ComfyUI running with models loaded in VRAM
↓
GPU Monitor: game_running = 0.0, vram_free = 6GB
↓
Resource-Aware Strategy: No action needed (count stays at 1)
↓
AI Model Manager: Status = models loaded (count = 1)
```

### 2. Game Launch Detected

```
Steam launches Cyberpunk 2077
↓
GPU Monitor: game_running = 1.0 (detects Steam child processes)
↓
Resource-Aware Strategy: Game detected → scale down (count = 0)
↓
AI Model Manager:
  - POST /interrupt (stop current jobs)
  - POST /free (unload models)
  - VRAM freed in ~3-5 seconds
↓
Game has full GPU access
```

### 3. Game Closes

```
User quits game
↓
GPU Monitor: game_running = 0.0 (Steam children reduced)
↓
Resource-Aware Strategy: No game → scale up (count = 1)
  (waits 30s cooldown to ensure game fully closed)
↓
AI Model Manager: Marks models as available
↓
Next ComfyUI request auto-loads models
```

## Troubleshooting

### Models not unloading

**Check ComfyUI is accessible:**
```bash
curl http://localhost:8188/queue
```

**Check autoscaler logs:**
```bash
./nomad-autoscaler agent -config=autoscaler.hcl -log-level=debug
```

**Verify NVIDIA driver:**
```bash
nvidia-smi
```

### Game not detected

**Check Steam process:**
```bash
ps aux | grep steam
```

**Try specific process monitoring:**
Add your game executable to `game_processes` config.

**Check autoscaler logs:**
Look for "Steam game activity detected" messages.

### Plugins not loading

**Verify plugins are built in:**
```bash
./nomad-autoscaler version
```

**Check plugin registration:**
Look for plugin initialization logs on startup.

## Performance Tuning

### Fast Response (Aggressive)

```hcl
evaluation_interval = "2s"    # Check every 2 seconds
cooldown            = "5s"    # Quick unload
cooldown_on_scale_up = "15s"  # Quick reload
```

### Conservative (Stable)

```hcl
evaluation_interval = "10s"   # Check every 10 seconds
cooldown            = "30s"   # Wait longer after unload
cooldown_on_scale_up = "60s"  # Give game time to settle
```

### Coexistence Mode

For light games that don't need full GPU:

```hcl
strategy "resource-aware" {
    vram_threshold_mb = "2048"      # Only unload if < 2GB free
    vram_coexist_mb = "6144"        # Keep loaded if > 6GB free
    allow_coexistence = "true"
}
```

## Advanced: Multiple Model Sizes

Create separate policies for different model types:

```hcl
# Large models - aggressive unloading
scaling "large-sdxl-models" {
    target "ai-model-manager" {
        comfyui_url = "http://localhost:8188"
        comfyui_enabled = "true"
    }
    policy {
        check "gpu_resources" {
            strategy "resource-aware" {
                vram_threshold_mb = "8192"  # Need 8GB free
            }
        }
    }
}

# Small models - can coexist
scaling "small-sd15-models" {
    target "ai-model-manager" {
        comfyui_url = "http://localhost:8189"  # Different instance
        comfyui_enabled = "true"
    }
    policy {
        check "gpu_resources" {
            strategy "resource-aware" {
                vram_threshold_mb = "2048"  # Only need 2GB
                allow_coexistence = "true"
            }
        }
    }
}
```

## Integration with Nomad Jobs

While the plugin works independently of Nomad job scheduling, you can also configure your AI application Nomad jobs for graceful handling:

```hcl
job "comfyui" {
  group "app" {
    count = 1

    task "comfyui" {
      driver = "docker"

      config {
        image = "comfyui:latest"
        ports = ["http"]
      }

      resources {
        device "nvidia/gpu" {
          count = 1
        }
        memory = 16384
      }

      # Allow time for model unloading on shutdown
      kill_timeout = "60s"
    }
  }
}
```

## FAQ

**Q: Does this work with Nomad's native GPU device plugin?**
A: Yes! The autoscaler manages model memory, not Nomad allocations. Your AI jobs stay running.

**Q: Can I use this without Steam?**
A: Yes! Set `monitor_steam = "false"` and add specific game processes to `game_processes`.

**Q: What if I want to manually trigger unloading?**
A: You can call the AI app APIs directly, or temporarily add a dummy process to `game_processes`.

**Q: Does this support multiple GPUs?**
A: Currently monitors GPU 0. Multi-GPU support can be added by querying specific GPU indices.

**Q: Can I use this on Windows?**
A: Windows support is planned. The main changes needed are in process monitoring.

## Contributing

Contributions are welcome! Areas for improvement:

- Windows process monitoring support
- Multi-GPU management
- Additional AI framework support (Ollama, LocalAI, etc.)
- VRAM usage prediction based on queue depth
- Integration with game launchers beyond Steam (Epic, GOG, etc.)

## License

This plugin suite is part of the Nomad Autoscaler project and follows the same MPL-2.0 license.
