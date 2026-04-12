package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/all3n/cstow/internal/config"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
)

func TestResolveDoctorCacheDirPrefersEnvOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", filepath.Join(home, "env-cache"))

	dir, err := resolveDoctorCacheDir(&config.Global{
		Cache: config.GlobalCache{
			Dir: "~/configured-cache",
		},
	})

	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "env-cache"), dir)
}

func TestResolveDoctorCacheDirExpandsConfiguredHomePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", "")

	dir, err := resolveDoctorCacheDir(&config.Global{
		Cache: config.GlobalCache{
			Dir: "~/configured-cache",
		},
	})

	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "configured-cache"), dir)
}

func TestResolveDoctorCacheDirFallsBackToDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_CACHE_DIR", "")

	dir, err := resolveDoctorCacheDir(nil)

	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".cstow", "cache"), dir)
}

func TestIsRegistryNotFoundErrorRecognizesOnlyMissingObjectCases(t *testing.T) {
	assert.True(t, isRegistryNotFoundError(fakeDoctorAPIError{
		code:    "NoSuchKey",
		message: "manifest missing",
	}))
	assert.True(t, isRegistryNotFoundError(errors.New("404 not found")))
	assert.False(t, isRegistryNotFoundError(fakeDoctorAPIError{
		code:    "AccessDenied",
		message: "signature mismatch",
	}))
	assert.False(t, isRegistryNotFoundError(fakeDoctorAPIError{
		code:    "NoSuchBucket",
		message: "bucket not found",
	}))
}

func TestCheckRegistrySkipsMissingProjectConfig(t *testing.T) {
	home := t.TempDir()
	workdir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CSTOW_REGISTRY_URL", "")
	t.Setenv("CSTOW_REGISTRY_KEY", "")
	t.Setenv("CSTOW_REGISTRY_SECRET", "")

	prevWD, err := os.Getwd()
	assert.NoError(t, err)
	assert.NoError(t, os.Chdir(workdir))
	t.Cleanup(func() {
		assert.NoError(t, os.Chdir(prevWD))
	})

	var out strings.Builder
	checkRegistry(&out)

	assert.Contains(t, out.String(), "NOT CONFIGURED")
	assert.NotContains(t, out.String(), "project config unreadable")
}

func TestCheckSourceBuildToolsReportsMixedAvailability(t *testing.T) {
	prevLookPath := doctorLookPath
	doctorLookPath = func(file string) (string, error) {
		switch file {
		case "git":
			return "/usr/bin/git", nil
		case "tar":
			return "/usr/bin/tar", nil
		case "ninja":
			return "/usr/bin/ninja", nil
		case "autoconf":
			return "/usr/bin/autoconf", nil
		case "automake":
			return "/usr/bin/automake", nil
		default:
			return "", errors.New("not found")
		}
	}
	t.Cleanup(func() { doctorLookPath = prevLookPath })

	var out strings.Builder
	checkSourceBuildTools(&out)
	got := out.String()

	assert.Contains(t, got, "[ ] Git: ✅ /usr/bin/git")
	assert.Contains(t, got, "[ ] Patch: ⚠️  NOT FOUND")
	assert.Contains(t, got, "[ ] Tar: ✅ /usr/bin/tar")
	assert.Contains(t, got, "[ ] Make: ⚠️  NOT FOUND")
	assert.Contains(t, got, "[ ] Ninja: ✅ /usr/bin/ninja")
	assert.Contains(t, got, "[ ] Autotools: ⚠️  PARTIAL")
	assert.Contains(t, got, "missing: autoreconf, libtoolize")
}

type fakeDoctorAPIError struct {
	code    string
	message string
}

func (e fakeDoctorAPIError) Error() string {
	return e.message
}

func (e fakeDoctorAPIError) ErrorCode() string {
	return e.code
}

func (e fakeDoctorAPIError) ErrorMessage() string {
	return e.message
}

func (e fakeDoctorAPIError) ErrorFault() smithy.ErrorFault {
	return smithy.FaultClient
}
