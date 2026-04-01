package builder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInstallDir(t *testing.T) {
	dir := InstallDir("/cache", "fmt", "10.2.1", "gcc13-cxx17-linux-x86_64")
	assert.Equal(t, "/cache/fmt/10.2.1/gcc13-cxx17-linux-x86_64", dir)
}

func TestGuessJobs(t *testing.T) {
	jobs := GuessJobs()
	assert.Greater(t, jobs, 0)
}
func TestIsCmakeInstalled(t *testing.T) {
	_ = IsCmakeInstalled()
}
