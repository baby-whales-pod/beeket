package libinstall

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// DetectBackend / detectBackend (table-driven, no process spawning needed for
// the darwin cases; linux/windows cases test the fallback to cpu only since
// CI doesn't have nvidia/rocm/vulkan).
// ---------------------------------------------------------------------------

func TestDetectBackend_Darwin(t *testing.T) {
	tests := []struct {
		goos    string
		goarch  string
		want    string
	}{
		{"darwin", "arm64", "metal"},
		{"darwin", "amd64", "cpu"},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.goarch, func(t *testing.T) {
			got := detectBackend(tt.goos, tt.goarch)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectBackend_UnknownOS(t *testing.T) {
	// Unknown OS should always fall back to cpu.
	got := detectBackend("plan9", "amd64")
	assert.Equal(t, "cpu", got)
}

func TestDetectBackend_LinuxFallbackCPU(t *testing.T) {
	// On a plain CI machine without nvidia/rocm/vulkan the result should be
	// cpu. We can't stub the probes here, so we just assert the function
	// returns a valid backend name.
	got := detectBackend("linux", "amd64")
	validBackends := map[string]bool{
		"cpu": true, "cuda": true, "rocm": true, "vulkan": true,
	}
	assert.True(t, validBackends[got], "unexpected backend: %q", got)
}

func TestDetectBackend_WindowsFallbackCPU(t *testing.T) {
	got := detectBackend("windows", "amd64")
	validBackends := map[string]bool{
		"cpu": true, "cuda": true, "rocm": true, "vulkan": true,
	}
	assert.True(t, validBackends[got], "unexpected backend: %q", got)
}

// TestDetectBackend_HostPlatform verifies that DetectBackend (the real
// exported function, using the actual runtime) returns a valid backend.
func TestDetectBackend_HostPlatform(t *testing.T) {
	got := DetectBackend()
	validBackends := map[string]bool{
		"cpu": true, "cuda": true, "rocm": true, "vulkan": true, "metal": true,
	}
	assert.True(t, validBackends[got],
		"DetectBackend() returned unexpected value %q on %s/%s",
		got, runtime.GOOS, runtime.GOARCH)
}

// ---------------------------------------------------------------------------
// resolveYzma
// ---------------------------------------------------------------------------

func TestResolveYzma_NotFound(t *testing.T) {
	// Override PATH and GOBIN so no yzma is reachable.
	t.Setenv("PATH", "/nonexistent")
	t.Setenv("GOBIN", "/nonexistent")
	t.Setenv("GOPATH", "/nonexistent")

	_, err := resolveYzma()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go install github.com/hybridgroup/yzma@latest",
		"error message should contain install instructions")
	assert.Contains(t, err.Error(), "--auto-install-lib",
		"error message should mention the flag")
}

func TestResolveYzma_FoundOnPath(t *testing.T) {
	// Create a fake yzma binary and add its directory to PATH.
	dir := t.TempDir()
	fakeYzma := filepath.Join(dir, yzmaBinary())
	require.NoError(t, os.WriteFile(fakeYzma, []byte("#!/bin/sh\n"), 0755))

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Ensure GOBIN / GOPATH don't inadvertently shadow our fake binary.
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")

	got, err := resolveYzma()
	require.NoError(t, err)
	assert.Equal(t, fakeYzma, got)
}

func TestResolveYzma_FoundViaGOBIN(t *testing.T) {
	dir := t.TempDir()
	fakeYzma := filepath.Join(dir, yzmaBinary())
	require.NoError(t, os.WriteFile(fakeYzma, []byte("#!/bin/sh\n"), 0755))

	// Remove yzma from PATH, put it in GOBIN.
	t.Setenv("PATH", "/nonexistent")
	t.Setenv("GOBIN", dir)
	t.Setenv("GOPATH", "")

	got, err := resolveYzma()
	require.NoError(t, err)
	assert.Equal(t, fakeYzma, got)
}

func TestResolveYzma_FoundViaGOPATH(t *testing.T) {
	gopath := t.TempDir()
	binDir := filepath.Join(gopath, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0755))
	fakeYzma := filepath.Join(binDir, yzmaBinary())
	require.NoError(t, os.WriteFile(fakeYzma, []byte("#!/bin/sh\n"), 0755))

	t.Setenv("PATH", "/nonexistent")
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", gopath)

	got, err := resolveYzma()
	require.NoError(t, err)
	assert.Equal(t, fakeYzma, got)
}

// ---------------------------------------------------------------------------
// findLibrary
// ---------------------------------------------------------------------------

func TestFindLibrary_Found(t *testing.T) {
	dir := t.TempDir()
	libPath := filepath.Join(dir, "libllama.so")
	require.NoError(t, os.WriteFile(libPath, []byte{}, 0644))

	got, ok := findLibrary(dir)
	assert.True(t, ok)
	assert.Equal(t, libPath, got)
}

func TestFindLibrary_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, ok := findLibrary(dir)
	assert.False(t, ok)
}

func TestFindLibrary_DylibFound(t *testing.T) {
	dir := t.TempDir()
	libPath := filepath.Join(dir, "libllama.dylib")
	require.NoError(t, os.WriteFile(libPath, []byte{}, 0644))

	got, ok := findLibrary(dir)
	assert.True(t, ok)
	assert.Equal(t, libPath, got)
}
