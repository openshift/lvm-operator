package util

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

const FileLockDir = "/var/lock/vgmanager"

// FileLock is a lock file used to ensure that only one instance of the operator is running,
// For example, when an old instance is still terminating its controllers, the new instance should not start.
// In this case the old instance will still hold the lock file and the new instance will wait for the lock to be released
// with WaitForLock.
type FileLock struct {
	file *os.File
}

func NewFileLock(name string) (*FileLock, error) {
	if err := os.Mkdir(FileLockDir, 0755); err != nil && !os.IsExist(err) {
		return nil, err
	}

	fileName := filepath.Join(FileLockDir, fmt.Sprintf("%s.lock", name))
	file, err := os.OpenFile(fileName, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return nil, err
	}
	return &FileLock{file: file}, nil
}

func (l *FileLock) WaitForLock(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.Info("Waiting for lock", "lockFile", l.file.Name())
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			err := l.TryLock()
			if err == nil {
				logger.Info("Lock acquired", "lockFile", l.file.Name())
				return nil
			}
			if !errors.Is(err, syscall.EAGAIN) {
				return fmt.Errorf("could not wait for lock because it was already locked by another process: %w", err)
			}
			logger.Info("Waiting for lock to be released", "lockFile", l.file.Name())
			time.Sleep(3 * time.Second)
		}
	}
}

func (l *FileLock) TryLock() error {
	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func (l *FileLock) Unlock() error {
	return syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
}

func (l *FileLock) Close() error {
	if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
		return err
	}
	return l.file.Close()
}
