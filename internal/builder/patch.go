package builder

import (
	"fmt"
	"os"
	"os/exec"
)

// ApplyPatch applies a patch file to the source directory using the external 'patch' command.
// It assumes -p1 by default as is common for C++ package patches.
func ApplyPatch(patchPath, sourceDir string) error {
	if _, err := os.Stat(patchPath); err != nil {
		return fmt.Errorf("patch file not found: %w", err)
	}

	cmd := exec.Command("patch", "-p1", "-i", patchPath)
	cmd.Dir = sourceDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("patch failed: %w: %s", err, string(out))
	}
	return nil
}

// IsPatchInstalled checks if the 'patch' command is available on PATH.
func IsPatchInstalled() bool {
	_, err := exec.LookPath("patch")
	return err == nil
}
