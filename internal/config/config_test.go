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

func TestApplyEnv_Backend(t *testing.T) {
	t.Setenv("BEEKET_BACKEND", "cuda")
	cfg := Defaults()
	ApplyEnv(&cfg)
	assert.Equal(t, "cuda", cfg.Runtime.Backend)
}

func TestApplyEnv_AutoInstallLib(t *testing.T) {
	for _, val := range []string{"true", "1"} {
		t.Setenv("BEEKET_AUTO_INSTALL_LIB", val)
		cfg := Defaults()
		ApplyEnv(&cfg)
		assert.True(t, cfg.Runtime.AutoInstallLib, "value=%q", val)
	}
}
