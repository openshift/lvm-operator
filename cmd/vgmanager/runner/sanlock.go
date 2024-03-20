package runner

import (
	"context"
	"fmt"
	"golang.org/x/sync/errgroup"
	"os"
	"os/exec"
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

	errs, ctx := errgroup.WithContext(ctx)

	errs.Go(func() error {
		args := []string{"daemon", "-D", "-w", "0", "-U", "root", "-G", "root", "-e", s.nodeName}
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
