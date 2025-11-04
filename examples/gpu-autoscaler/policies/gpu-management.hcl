# GPU Management Policy for ComfyUI
# This policy automatically unloads AI models when games are detected
# and reloads them when gaming is complete.

scaling "comfyui-gpu-manager" {
    enabled = true

    # State: 0 = models unloaded, 1 = models loaded
    min = 0
    max = 1

    # Use horizontal type (though this is a custom implementation)
    type = "horizontal"

    policy {
        # How often to check for game launches (in seconds)
        # Lower values = faster response, higher CPU usage
        evaluation_interval = "3s"

        # Cooldown after scaling down (unloading models)
        # Prevents rapid toggling
        cooldown = "10s"

        # Cooldown after scaling up (marking models as available)
        # Gives game time to fully close before reloading
        cooldown_on_scale_up = "30s"

        check "gpu_resources" {
            # Use the GPU Monitor APM plugin
            source = "gpu-monitor"

            # Query mode:
            # - "system" returns game running status (recommended)
            # - "game_running" same as system
            # - "vram_free" returns free VRAM in MB
            # - "gpu_utilization" returns GPU utilization %
            query = "system"

            # Query window (how far back to look for metrics)
            query_window = "1m"

            # Use the Resource-Aware strategy
            strategy "resource-aware" {
                # Enable Steam process monitoring
                monitor_steam = "true"

                # Minimum free VRAM (MB) before unloading models
                # If VRAM drops below this and game is running, unload models
                vram_threshold_mb = "4096"

                # VRAM threshold for coexistence mode (MB)
                # If VRAM is above this, allow AI and games to coexist
                vram_coexist_mb = "8192"

                # Minimum free system RAM (MB)
                ram_threshold_mb = "8192"

                # Allow AI workloads during light gaming if resources permit
                # Set to "false" for strict game priority
                allow_coexistence = "false"
            }
        }

        # AI Model Manager Target - manages model loading/unloading
        target "ai-model-manager" {
            # ComfyUI Configuration
            comfyui_url = "http://localhost:8188"
            comfyui_enabled = "true"

            # Stable Diffusion WebUI Configuration (optional)
            # Enable if you're running SD WebUI
            sdwebui_url = "http://localhost:7860"
            sdwebui_enabled = "false"

            # InvokeAI Configuration (optional)
            # Enable if you're running InvokeAI
            invokeai_url = "http://localhost:9090"
            invokeai_enabled = "false"

            # Interrupt running inference jobs before unloading
            # Recommended: true for faster unloading
            interrupt_running_jobs = "true"

            # Timeout (seconds) for waiting for jobs to complete
            wait_timeout_seconds = "30"
        }
    }
}
