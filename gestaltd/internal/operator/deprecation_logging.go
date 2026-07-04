package operator

import (
	"log/slog"

	"github.com/valon-technologies/gestalt/server/internal/config"
)

// WithDeprecationLogger installs the sink used to surface config deprecation
// warnings during lifecycle operations.
func (l *Lifecycle) WithDeprecationLogger(log func(string)) *Lifecycle {
	l.deprecationLogger = log
	return l
}

func (l *Lifecycle) emitDeprecationWarnings(cfg *config.Config) {
	if l == nil || cfg == nil {
		return
	}
	warnings := cfg.DeprecationWarnings()
	if len(warnings) == 0 {
		return
	}
	if l.emittedDeprecationWarnings == nil {
		l.emittedDeprecationWarnings = make(map[string]struct{}, len(warnings))
	}
	for _, warning := range warnings {
		if _, seen := l.emittedDeprecationWarnings[warning]; seen {
			continue
		}
		l.emittedDeprecationWarnings[warning] = struct{}{}
		if l.deprecationLogger != nil {
			l.deprecationLogger(warning)
			continue
		}
		slog.Warn(warning)
	}
}
