package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/sync/errgroup"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type sanlockRunner struct {
	nodeName string
}

var _ manager.Runnable = &sanlockRunner{}

func NewSanlockRunner(nodeName string) manager.Runnable {
	return &sanlockRunner{
		nodeName: nodeName,
	}
}

// Start implements controller-runtime's manager.Runnable.
func (s *sanlockRunner) Start(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.Info("Starting sanlock runnable")

	if err := os.RemoveAll("/run/sanlock"); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove sanlock socket: %w", err)
	}

	errs, ctx := errgroup.WithContext(ctx)

	errs.Go(func() error {
		args := []string{"daemon", "-Q", "1", "-D", "-w", "0", "-U", "root", "-G", "root", "-e", s.nodeName}
		cmd := exec.Command("sanlock", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to run sanlock daemon: %w", err)
		}

		return nil
	})

	return errs.Wait()
}
