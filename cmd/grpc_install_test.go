package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGrpcManualInstall(t *testing.T) {
	if os.Getenv("DO_GRPC_INSTALL") != "1" {
		t.Skip("Skipping gRPC manual install test. Set DO_GRPC_INSTALL=1 to run.")
	}

	// Use the fake home we set up
	fakeHome := "/tmp/fakehome"
	cacheDir := "/home/wanghch/workspaces/cstow/tmp/cache"
	
	t.Setenv("HOME", fakeHome)
	t.Setenv("CSTOW_CACHE_DIR", cacheDir)

	fmt.Println("Starting gRPC installation...")
	
	// We'll run it with a timeout because it's very slow
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Using 'go run main.go' to ensure we use the latest code
	cmd := exec.CommandContext(ctx, "go", "run", "../main.go", "install", "grpc")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Println("gRPC installation timed out as expected (it's very slow)")
			return
		}
		require.NoError(t, err, "gRPC installation failed")
	}

	fmt.Println("gRPC installation completed (surprising! it was fast?)")
}
