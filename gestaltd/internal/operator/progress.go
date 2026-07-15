package operator

// LifecycleProgress receives command-level lifecycle events from lock, sync, and
// serve artifact preparation. Operators never render these events; command
// boundaries decide how (or whether) to present them.
type LifecycleProgress func(LifecycleProgressEvent)

type LifecycleProgressOperation string

const (
	LifecycleOperationLock  LifecycleProgressOperation = "lock"
	LifecycleOperationSync  LifecycleProgressOperation = "sync"
	LifecycleOperationServe LifecycleProgressOperation = "serve"
)

type LifecycleProgressPhase string

const (
	LifecyclePhaseConfig   LifecycleProgressPhase = "config"
	LifecyclePhaseLock     LifecycleProgressPhase = "lock"
	LifecyclePhaseInstall  LifecycleProgressPhase = "install"
	LifecyclePhaseComplete LifecycleProgressPhase = "complete"
)

type LifecycleProgressStatus string

const (
	LifecycleProgressStarted   LifecycleProgressStatus = "started"
	LifecycleProgressCompleted LifecycleProgressStatus = "completed"
	LifecycleProgressNoop      LifecycleProgressStatus = "noop"
)

// LifecycleProgressEvent contains concise operation state. A completed write
// may include its output path.
type LifecycleProgressEvent struct {
	Operation LifecycleProgressOperation
	Phase     LifecycleProgressPhase
	Status    LifecycleProgressStatus

	Subject string
	Reason  string
	Path    string
}

func (l *Lifecycle) emitProgress(event LifecycleProgressEvent) {
	if l == nil || l.progress == nil {
		return
	}
	l.progress(event)
}
