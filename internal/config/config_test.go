package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	assert.Equal(t, "127.0.0.1", cfg.Server.Host)
	assert.Equal(t, 11435, cfg.Server.Port)
	assert.Equal(t, "auto", cfg.Runtime.Backend)
	assert.Equal(t, -1, cfg.Runtime.GPULayers)
	assert.Equal(t, 1, cfg.Runtime.NumParallel)
	assert.Equal(t, 3, cfg.Runtime.MaxLoaded)
	assert.Equal(t, "5m", cfg.Runtime.KeepAlive)
	assert.Equal(t, 4096, cfg.Runtime.ContextSize)
	assert.False(t, cfg.Runtime.AutoInstallLib)
	assert.False(t, cfg.Runtime.LibUpgrade)
	assert.Equal(t, "", cfg.Runtime.LibVersion)
}

func TestValidate_ValidBackends(t *testing.T) {
	for _, b := range []string{"auto", "cpu", "cuda", "metal", "vulkan", "rocm"} {
		cfg := Defaults()
		cfg.Runtime.Backend = b
		assert.NoError(t, Validate(&cfg), "backend=%q should be valid", b)
	}
}

func TestValidate_InvalidBackend(t *testing.T) {
	cfg := Defaults()
	cfg.Runtime.Backend = "opengl"
	assert.Error(t, Validate(&cfg))
}

// ---------------------------------------------------------------------------
// ResolveLibDir priority chain
// ---------------------------------------------------------------------------

func TestResolveLibDir_ExplicitFlag(t *testing.T) {
	cfg := Defaults()
	cfg.Paths.LibDir = "/my/explicit/lib"
	assert.Equal(t, "/my/explicit/lib", ResolveLibDir(&cfg))
}

func TestResolveLibDir_BeeketLibDirEnv(t *testing.T) {
	t.Setenv("BEEKET_LIB_DIR", "/env/lib")
	t.Setenv("YZMA_LIB", "")
	cfg := Defaults()
	assert.Equal(t, "/env/lib", ResolveLibDir(&cfg))
}

func TestResolveLibDir_YzmaLibEnv(t *testing.T) {
	t.Setenv("BEEKET_LIB_DIR", "")
	t.Setenv("YZMA_LIB", "/yzma/lib")
	cfg := Defaults()
	assert.Equal(t, "/yzma/lib", ResolveLibDir(&cfg))
}

func TestResolveLibDir_DefaultDataDir(t *testing.T) {
	t.Setenv("BEEKET_LIB_DIR", "")
	t.Setenv("YZMA_LIB", "")
	t.Setenv("BEEKET_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".local", "share", "beeket", "lib")

	cfg := Defaults()
	assert.Equal(t, expected, ResolveLibDir(&cfg))
}

func TestResolveLibDir_XDGDataHome(t *testing.T) {
	t.Setenv("BEEKET_LIB_DIR", "")
	t.Setenv("YZMA_LIB", "")
	t.Setenv("BEEKET_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", "/xdg/data")

	cfg := Defaults()
	assert.Equal(t, "/xdg/data/beeket/lib", ResolveLibDir(&cfg))
}

// ---------------------------------------------------------------------------
// ApplyEnv — complete coverage of all env vars
// ---------------------------------------------------------------------------

func TestApplyEnv_Host(t *testing.T) {
	t.Setenv("BEEKET_HOST", "0.0.0.0")
	cfg := Defaults()
	ApplyEnv(&cfg)
	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
}

func TestApplyEnv_Port(t *testing.T) {
	t.Setenv("BEEKET_PORT", "9000")
	cfg := Defaults()
	ApplyEnv(&cfg)
	assert.Equal(t, 9000, cfg.Server.Port)
}

func TestApplyEnv_Port_InvalidIgnored(t *testing.T) {
	t.Setenv("BEEKET_PORT", "notanumber")
	cfg := Defaults()
	ApplyEnv(&cfg)
	// Invalid value must be ignored; default port must remain.
	assert.Equal(t, 11435, cfg.Server.Port)
}

func TestApplyEnv_DataDir(t *testing.T) {
	t.Setenv("BEEKET_DATA_DIR", "/custom/data")
	cfg := Defaults()
	ApplyEnv(&cfg)
	assert.Equal(t, "/custom/data", cfg.Paths.DataDir)
}

func TestApplyEnv_LibDir(t *testing.T) {
	t.Setenv("BEEKET_LIB_DIR", "/custom/lib")
	cfg := Defaults()
	ApplyEnv(&cfg)
	assert.Equal(t, "/custom/lib", cfg.Paths.LibDir)
}

func TestApplyEnv_Backend(t *testing.T) {
	t.Setenv("BEEKET_BACKEND", "cuda")
	cfg := Defaults()
	ApplyEnv(&cfg)
	assert.Equal(t, "cuda", cfg.Runtime.Backend)
}

func TestApplyEnv_GPULayers(t *testing.T) {
	t.Setenv("BEEKET_GPU_LAYERS", "20")
	cfg := Defaults()
	ApplyEnv(&cfg)
	assert.Equal(t, 20, cfg.Runtime.GPULayers)
}

func TestApplyEnv_NumParallel(t *testing.T) {
	t.Setenv("BEEKET_NUM_PARALLEL", "4")
	cfg := Defaults()
	ApplyEnv(&cfg)
	assert.Equal(t, 4, cfg.Runtime.NumParallel)
}

func TestApplyEnv_MaxLoadedModels(t *testing.T) {
	t.Setenv("BEEKET_MAX_LOADED_MODELS", "5")
	cfg := Defaults()
	ApplyEnv(&cfg)
	assert.Equal(t, 5, cfg.Runtime.MaxLoaded)
}

func TestApplyEnv_KeepAlive(t *testing.T) {
	t.Setenv("BEEKET_KEEP_ALIVE", "10m")
	cfg := Defaults()
	ApplyEnv(&cfg)
	assert.Equal(t, "10m", cfg.Runtime.KeepAlive)
}

func TestApplyEnv_ContextSize(t *testing.T) {
	t.Setenv("BEEKET_CONTEXT_SIZE", "8192")
	cfg := Defaults()
	ApplyEnv(&cfg)
	assert.Equal(t, 8192, cfg.Runtime.ContextSize)
}

func TestApplyEnv_AutoInstallLib_True(t *testing.T) {
	for _, val := range []string{"true", "1"} {
		t.Setenv("BEEKET_AUTO_INSTALL_LIB", val)
		cfg := Defaults()
		ApplyEnv(&cfg)
		assert.True(t, cfg.Runtime.AutoInstallLib, "value=%q should enable", val)
	}
}

func TestApplyEnv_AutoInstallLib_False(t *testing.T) {
	// Start with AutoInstallLib already enabled; env should be able to disable it.
	for _, val := range []string{"false", "0"} {
		t.Setenv("BEEKET_AUTO_INSTALL_LIB", val)
		cfg := Defaults()
		cfg.Runtime.AutoInstallLib = true // pre-enable
		ApplyEnv(&cfg)
		assert.False(t, cfg.Runtime.AutoInstallLib, "value=%q should disable", val)
	}
}

func TestApplyEnv_LogLevel(t *testing.T) {
	t.Setenv("BEEKET_LOG_LEVEL", "debug")
	cfg := Defaults()
	ApplyEnv(&cfg)
	assert.Equal(t, "debug", cfg.Log.Level)
}

func TestApplyEnv_LogFormat(t *testing.T) {
	t.Setenv("BEEKET_LOG_FORMAT", "json")
	cfg := Defaults()
	ApplyEnv(&cfg)
	assert.Equal(t, "json", cfg.Log.Format)
}
