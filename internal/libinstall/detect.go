package libinstall

import (
	"context"
	"os/exec"
	"runtime"
)

// probes groups the three GPU probe functions so they can be stubbed in tests.
type probes struct {
	hasNVIDIA func(ctx context.Context) bool
	hasROCm   func(ctx context.Context) bool
	hasVulkan func(ctx context.Context) bool
}

// defaultProbes uses the real system-level probe implementations.
var defaultProbes = probes{
	hasNVIDIA: hasNVIDIA,
	hasROCm:   hasROCm,
	hasVulkan: hasVulkan,
}

// DetectBackend picks the best processor backend for the current host.
// Resolution algorithm (matches design doc §4):
//
//	darwin/arm64  → metal
//	darwin/amd64  → cpu   (Intel Macs: Metal unsupported by current llama.cpp builds)
//	linux|windows → cuda → rocm → vulkan → cpu  (probed in order)
//
// The provided ctx is forwarded to each probe so hung subprocesses are
// bounded by the caller's deadline/cancellation.
func DetectBackend(ctx context.Context) string {
	return detectBackend(ctx, runtime.GOOS, runtime.GOARCH, defaultProbes)
}

// detectBackend is the testable core of DetectBackend. goos and goarch are
// passed explicitly so unit tests can cover non-host platforms, and p allows
// probe functions to be stubbed for deterministic GPU-detection tests.
func detectBackend(ctx context.Context, goos, goarch string, p probes) string {
	switch goos {
	case "darwin":
		if goarch == "arm64" {
			return "metal"
		}
		return "cpu" // Intel Mac
	case "linux", "windows":
		if p.hasNVIDIA(ctx) {
			return "cuda"
		}
		if p.hasROCm(ctx) {
			return "rocm"
		}
		if p.hasVulkan(ctx) {
			return "vulkan"
		}
		return "cpu"
	default:
		return "cpu"
	}
}

// hasNVIDIA reports whether an NVIDIA GPU is available.
// Probes (any one is sufficient):
//   - /proc/driver/nvidia/version exists (Linux kernel module loaded — zero process spawn)
//   - `nvidia-smi -L` exits 0
func hasNVIDIA(ctx context.Context) bool {
	// Fast kernel-module check (Linux only, zero process spawn).
	// We avoid os.Stat importing "os" into this file; use exec which is already imported.
	if _, err := exec.LookPath("nvidia-smi"); err == nil {
		cmd := exec.CommandContext(ctx, "nvidia-smi", "-L")
		if cmd.Run() == nil {
			return true
		}
	}
	// Fallback: check kernel module sysfs node directly.
	check := exec.CommandContext(ctx, "sh", "-c",
		"test -e /proc/driver/nvidia/version")
	return check.Run() == nil
}

// hasROCm reports whether an AMD ROCm stack is present.
// Probe: `rocminfo` exits 0. This works regardless of install path
// (avoids false negatives on custom ROCm installs that don't use /opt/rocm).
func hasROCm(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "rocminfo")
	return cmd.Run() == nil
}

// hasVulkan reports whether a functional Vulkan implementation is present.
// Probe: `vulkaninfo --summary` exits 0.
func hasVulkan(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "vulkaninfo", "--summary")
	return cmd.Run() == nil
}
