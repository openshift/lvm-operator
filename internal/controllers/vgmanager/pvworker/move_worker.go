package pvworker

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

var ErrWorkerAlreadySyncingPVs = errors.New("worker is already syncing PVs")

type MoveExtentFn = func(ctx context.Context, from []string, to []string) error

type AsyncExtentMover interface {
	Sync(from, to []string) error
	IsSyncing() bool
	Status() *SyncStatus
	Reset() error
}

type SyncStatus struct {
	state    SyncState
	err      error
	start    time.Time
	end      time.Time
	from, to []string
}

func (s *SyncStatus) State() SyncState {
	return s.state
}

func (s *SyncStatus) Error() error {
	return s.err
}

func (s *SyncStatus) Operation() (from []string, to []string) {
	return s.from, s.to
}

// SyncDuration returns the duration of the sync operation since it started and until it finished.
func (s *SyncStatus) SyncDuration() time.Duration {
	if s.end.IsZero() {
		return time.Since(s.start)
	}
	return s.end.Sub(s.start)
}

type SyncState string

const (
	SyncStateSyncing SyncState = "syncing"
	SyncStateError   SyncState = "error"
	SyncStateIdle    SyncState = "idle"
	SyncStateDone    SyncState = "done"
)

func NewAsyncExtentMover(move MoveExtentFn) AsyncExtentMover {
	worker := &asyncExtentMover{
		syncStatus: atomic.Pointer[SyncStatus]{},
		pvMove:     move,
	}
	worker.syncStatus.Store(&SyncStatus{state: SyncStateIdle})
	return worker
}

type asyncExtentMover struct {
	syncStatus atomic.Pointer[SyncStatus]
	pvMove     MoveExtentFn
}

func (w *asyncExtentMover) Sync(from, to []string) error {
	if w.IsSyncing() {
		return ErrWorkerAlreadySyncingPVs
	}

	if w.syncStatus.Load().state == SyncStateError {
		return fmt.Errorf("cannot start new sync operation when previous one failed, "+
			"explicit reset is necessary: %w", w.syncStatus.Load().err)
	}

	w.startSync(from, to)
	go func(ctx context.Context, from, to []string) {
		err := w.pvMove(ctx, from, to)
		w.finishSync(err)
	}(context.Background(), from, to)

	return nil
}

func (w *asyncExtentMover) Status() *SyncStatus {
	return w.syncStatus.Load()
}

func (w *asyncExtentMover) IsSyncing() bool {
	return w.syncStatus.Load().state == SyncStateSyncing
}

func (w *asyncExtentMover) startSync(from, to []string) {
	w.syncStatus.Store(&SyncStatus{
		state: SyncStateSyncing,
		start: time.Now(),
		from:  from,
		to:    to,
	})
}

func (w *asyncExtentMover) finishSync(err error) {
	var state SyncState
	if err != nil {
		state = SyncStateError
	} else {
		state = SyncStateDone
	}
	status := w.syncStatus.Load()
	status.err = err
	status.state = state
	status.end = time.Now()
	w.syncStatus.Store(status)
}

func (w *asyncExtentMover) Reset() error {
	if w.IsSyncing() {
		return fmt.Errorf("cannot reset running worker: %w", ErrWorkerAlreadySyncingPVs)
	}

	w.syncStatus.Store(&SyncStatus{state: SyncStateIdle})

	return nil
}
