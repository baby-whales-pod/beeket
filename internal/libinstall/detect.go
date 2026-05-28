package libinstall

import (
	"os"
	"os/exec"
	"runtime"
)

// DetectBackend picks the best processor backend for the current host.
// Resolution algorithm (matches design doc §4):
//
//	darwin/arm64  → metal
//	darwin/amd64  → cpu   (Intel Macs: Metal unsupported by current llama.cpp builds)
//	linux|windows → cuda → rocm → vulkan → cpu  (probed in order)
func DetectBackend() string {
	return detectBackend(runtime.GOOS, runtime.GOARCH)
}

// detectBackend is the testable core of DetectBackend. goos and goarch are
// passed explicitly so unit tests can cover non-host platforms.
func detectBackend(goos, goarch string) string {
	switch goos {
	case "darwin":
		if goarch == "arm64" {
			return "metal"
		}
		return "cpu" // Intel Mac
	case "linux", "windows":
		if hasNVIDIA() {
			return "cuda"
		}
		if hasROCm() {
			return "rocm"
		}
		if hasVulkan() {
			return "vulkan"
		}
		return "cpu"
	default:
		return "cpu"
	}
}

// hasNVIDIA reports whether an NVIDIA GPU is available.
// Probes (any one is sufficient):
//   - `nvidia-smi -L` exits 0
//   - /proc/driver/nvidia/version exists (Linux kernel module loaded)
func hasNVIDIA() bool {
	// Fast kernel-module check (Linux only, zero process spawn).
	if _, err := os.Stat("/proc/driver/nvidia/version"); err == nil {
		return true
	}
	// nvidia-smi is available on Linux and Windows.
	cmd := exec.Command("nvidia-smi", "-L")
	return cmd.Run() == nil
}

// hasROCm reports whether an AMD ROCm stack is present.
// Probes:
//   - /opt/rocm directory exists (standard install location)
//   - `rocminfo` exits 0 (confirms functional stack)
func hasROCm() bool {
	if _, err := os.Stat("/opt/rocm"); err != nil {
		return false
	}
	cmd := exec.Command("rocminfo")
	return cmd.Run() == nil
}

// hasVulkan reports whether a functional Vulkan implementation is present.
// Probe: `vulkaninfo --summary` exits 0.
func hasVulkan() bool {
	cmd := exec.Command("vulkaninfo", "--summary")
	return cmd.Run() == nil
}
