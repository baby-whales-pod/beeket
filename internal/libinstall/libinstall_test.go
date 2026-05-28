package libinstall

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// detectBackend — table-driven with probe stubs for deterministic results
// ---------------------------------------------------------------------------

// neverProbe is a probe stub that always returns false (no GPU detected).
func neverProbe(_ context.Context) bool { return false }

// alwaysProbe is a probe stub that always returns true.
func alwaysProbe(_ context.Context) bool { return true }

func TestDetectBackend_Darwin(t *testing.T) {
	tests := []struct {
		goos   string
		goarch string
		want   string
	}{
		{"darwin", "arm64", "metal"},
		{"darwin", "amd64", "cpu"},
	}
	p := probes{hasNVIDIA: neverProbe, hasROCm: neverProbe, hasVulkan: neverProbe}
	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.goarch, func(t *testing.T) {
			got := detectBackend(context.Background(), tt.goos, tt.goarch, p)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectBackend_UnknownOS(t *testing.T) {
	p := probes{hasNVIDIA: neverProbe, hasROCm: neverProbe, hasVulkan: neverProbe}
	got := detectBackend(context.Background(), "plan9", "amd64", p)
	assert.Equal(t, "cpu", got)
}

func TestDetectBackend_Linux_NoCPU_NVIDIAWins(t *testing.T) {
	p := probes{hasNVIDIA: alwaysProbe, hasROCm: alwaysProbe, hasVulkan: alwaysProbe}
	got := detectBackend(context.Background(), "linux", "amd64", p)
	assert.Equal(t, "cuda", got)
}

func TestDetectBackend_Linux_ROCmFallback(t *testing.T) {
	p := probes{hasNVIDIA: neverProbe, hasROCm: alwaysProbe, hasVulkan: alwaysProbe}
	got := detectBackend(context.Background(), "linux", "amd64", p)
	assert.Equal(t, "rocm", got)
}

func TestDetectBackend_Linux_VulkanFallback(t *testing.T) {
	p := probes{hasNVIDIA: neverProbe, hasROCm: neverProbe, hasVulkan: alwaysProbe}
	got := detectBackend(context.Background(), "linux", "amd64", p)
	assert.Equal(t, "vulkan", got)
}

func TestDetectBackend_Linux_CPUFallback(t *testing.T) {
	// All probes return false → must be exactly cpu.
	p := probes{hasNVIDIA: neverProbe, hasROCm: neverProbe, hasVulkan: neverProbe}
	got := detectBackend(context.Background(), "linux", "amd64", p)
	assert.Equal(t, "cpu", got)
}

func TestDetectBackend_Windows_CPUFallback(t *testing.T) {
	p := probes{hasNVIDIA: neverProbe, hasROCm: neverProbe, hasVulkan: neverProbe}
	got := detectBackend(context.Background(), "windows", "amd64", p)
	assert.Equal(t, "cpu", got)
}

// TestDetectBackend_HostPlatform verifies that the exported DetectBackend
// (using real runtime GOOS/GOARCH and real probes) returns a valid backend.
func TestDetectBackend_HostPlatform(t *testing.T) {
	got := DetectBackend(context.Background())
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
	dir := t.TempDir()
	fakeYzma := filepath.Join(dir, yzmaBinary())
	require.NoError(t, os.WriteFile(fakeYzma, []byte("#!/bin/sh\n"), 0755))

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
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
// findOneOf / findLibrary
// ---------------------------------------------------------------------------

func TestFindOneOf_LlamaFound(t *testing.T) {
	dir := t.TempDir()
	libPath := filepath.Join(dir, "libllama.so")
	require.NoError(t, os.WriteFile(libPath, []byte{}, 0644))

	got, ok := findOneOf(dir, llamaLibNames)
	assert.True(t, ok)
	assert.Equal(t, libPath, got)
}

func TestFindOneOf_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, ok := findOneOf(dir, llamaLibNames)
	assert.False(t, ok)
}

func TestFindOneOf_DylibFound(t *testing.T) {
	dir := t.TempDir()
	libPath := filepath.Join(dir, "libllama.dylib")
	require.NoError(t, os.WriteFile(libPath, []byte{}, 0644))

	got, ok := findOneOf(dir, llamaLibNames)
	assert.True(t, ok)
	assert.Equal(t, libPath, got)
}

func TestFindLibrary_BackwardCompat(t *testing.T) {
	dir := t.TempDir()
	libPath := filepath.Join(dir, "libllama.so")
	require.NoError(t, os.WriteFile(libPath, []byte{}, 0644))

	got, ok := findLibrary(dir)
	assert.True(t, ok)
	assert.Equal(t, libPath, got)
}

// ---------------------------------------------------------------------------
// Idempotency: both libllama + libmtmd required
// ---------------------------------------------------------------------------

func TestEnsure_IdempotentSkipRequiresBothLibs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake yzma not supported on Windows")
	}

	dir := t.TempDir()
	// Only libllama present — should NOT skip install.
	llamaPath := filepath.Join(dir, "libllama.so")
	require.NoError(t, os.WriteFile(llamaPath, []byte{}, 0644))

	// Create fake yzma that records its invocation and exits 0.
	binDir := t.TempDir()
	fakeYzma := filepath.Join(binDir, "yzma")
	script := "#!/bin/sh\necho called \"$@\"\nmkdir -p \"$3\"\ntouch \"$3/libllama.so\" \"$3/libmtmd.so\"\n"
	require.NoError(t, os.WriteFile(fakeYzma, []byte(script), 0755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout bytes.Buffer
	_, err := Ensure(context.Background(), Options{
		LibDir:  dir,
		Backend: "cpu",
		Logger:  nil,
		Stdout:  &stdout,
	})
	require.NoError(t, err)
	// yzma should have been invoked because mtmd was missing.
	assert.Contains(t, stdout.String(), "called")
}

func TestEnsure_IdempotentSkipWhenBothPresent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake yzma not supported on Windows")
	}

	dir := t.TempDir()
	// Both libraries present — should skip yzma entirely.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "libllama.so"), []byte{}, 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "libmtmd.so"), []byte{}, 0644))

	// Fake yzma that exits 1 (should never be called).
	binDir := t.TempDir()
	fakeYzma := filepath.Join(binDir, "yzma")
	require.NoError(t, os.WriteFile(fakeYzma, []byte("#!/bin/sh\nexit 1\n"), 0755))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := Ensure(context.Background(), Options{
		LibDir:  dir,
		Backend: "cpu",
	})
	require.NoError(t, err) // yzma exit 1 must NOT have been hit
}

// ---------------------------------------------------------------------------
// Ensure — end-to-end integration with fake yzma
// ---------------------------------------------------------------------------

func TestEnsure_EndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake yzma not supported on Windows")
	}

	libDir := t.TempDir()
	binDir := t.TempDir()

	// Fake yzma: record all args and create both libs so subsequent checks pass.
	fakeYzma := filepath.Join(binDir, "yzma")
	script := "#!/bin/sh\necho \"$@\"\nmkdir -p \"$3\"\ntouch \"$3/libllama.so\" \"$3/libmtmd.so\"\n"
	require.NoError(t, os.WriteFile(fakeYzma, []byte(script), 0755))

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")

	var stdout bytes.Buffer
	backend, err := Ensure(context.Background(), Options{
		LibDir:  libDir,
		Backend: "cpu",
		Version: "b1234",
		Upgrade: true,
		Stdout:  &stdout,
	})
	require.NoError(t, err)
	assert.Equal(t, "cpu", backend)

	output := stdout.String()
	// Verify all expected flags were passed to yzma.
	assert.Contains(t, output, "--lib")
	assert.Contains(t, output, libDir)
	assert.Contains(t, output, "--processor")
	assert.Contains(t, output, "cpu")
	assert.Contains(t, output, "--version")
	assert.Contains(t, output, "b1234")
	assert.Contains(t, output, "--upgrade")
}

func TestEnsure_EndToEnd_NoVersionNoUpgrade(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake yzma not supported on Windows")
	}

	libDir := t.TempDir()
	binDir := t.TempDir()

	fakeYzma := filepath.Join(binDir, "yzma")
	script := "#!/bin/sh\necho \"$@\"\nmkdir -p \"$3\"\ntouch \"$3/libllama.so\" \"$3/libmtmd.so\"\n"
	require.NoError(t, os.WriteFile(fakeYzma, []byte(script), 0755))

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")

	var stdout bytes.Buffer
	_, err := Ensure(context.Background(), Options{
		LibDir:  libDir,
		Backend: "metal",
		Stdout:  &stdout,
	})
	require.NoError(t, err)

	output := stdout.String()
	assert.NotContains(t, output, "--version")
	assert.NotContains(t, output, "--upgrade")
	assert.Contains(t, output, "--processor")
	assert.Contains(t, output, "metal")
}

// TestEnsure_YZMA_LIB_Dedup verifies that YZMA_LIB is not duplicated in the
// child environment when it is already set in the parent.
func TestEnsure_YZMA_LIB_Dedup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake yzma not supported on Windows")
	}

	libDir := t.TempDir()
	binDir := t.TempDir()

	// Fake yzma: print its environment, one var per line.
	fakeYzma := filepath.Join(binDir, "yzma")
	script := "#!/bin/sh\nenv\nmkdir -p \"$3\"\ntouch \"$3/libllama.so\" \"$3/libmtmd.so\"\n"
	require.NoError(t, os.WriteFile(fakeYzma, []byte(script), 0755))

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	t.Setenv("YZMA_LIB", "/old/path") // pre-existing value

	var stdout bytes.Buffer
	_, err := Ensure(context.Background(), Options{
		LibDir:  libDir,
		Backend: "cpu",
		Stdout:  &stdout,
	})
	require.NoError(t, err)

	// Count how many times YZMA_LIB appears in the child environment.
	count := strings.Count(stdout.String(), "YZMA_LIB=")
	assert.Equal(t, 1, count, "YZMA_LIB must appear exactly once in child env")
	// And it must point to the new libDir, not the old path.
	assert.Contains(t, stdout.String(), "YZMA_LIB="+libDir)
}

func TestEnsure_MissingLibDir(t *testing.T) {
	_, err := Ensure(context.Background(), Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LibDir must not be empty")
}

func TestEnsure_YzmaNotFound(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")

	_, err := Ensure(context.Background(), Options{
		LibDir:  t.TempDir(),
		Backend: "cpu",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "go install github.com/hybridgroup/yzma@latest")
}
