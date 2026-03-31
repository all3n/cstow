package hooks

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/all3n/cstow/internal/config"
)

// Runner executes hook scripts
type Runner struct {
	hooks *config.Hooks
	dir   string
}

// New creates a hook runner
func New(hooks *config.Hooks, dir string) *Runner {
	return &Runner{hooks: hooks, dir: dir}
}

// Run executes a hook by name (pre-build, post-build, pre-publish, post-publish)
func (r *Runner) Run(name string) error {
	if r.hooks == nil {
		return nil
	}

	var script string
	switch name {
	case "pre-build":
		script = r.hooks.PreBuild
	case "post-build":
		script = r.hooks.PostBuild
	case "pre-publish":
		script = r.hooks.PrePublish
	case "post-publish":
		script = r.hooks.PostPublish
	default:
		return nil
	}

	if script == "" {
		return nil
	}

	fmt.Printf(">> running hook %s: %s\n", name, script)
	cmd := exec.Command("sh", "-c", script)
	cmd.Dir = r.dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hook %s failed: %w", name, err)
	}
	return nil
}
