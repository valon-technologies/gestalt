package daemon

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type commandProgress struct {
	activity *TerminalActivity
	started  time.Time
}

func startCommandProgress(format string, args ...any) *commandProgress {
	activity := currentCLIReporter().Start(fmt.Sprintf(format, args...) + "...")
	return &commandProgress{activity: activity, started: activity.started}
}

func (p *commandProgress) done(format string, args ...any) {
	if p == nil {
		return
	}
	message := fmt.Sprintf(format, args...) + " in " + elapsedSince(p.started)
	if p.activity != nil {
		p.activity.Finish(message)
		p.activity = nil
		return
	}
	currentCLIReporter().Status(message)
}

func progressStatus(format string, args ...any) {
	currentCLIReporter().Status(fmt.Sprintf(format, args...))
}

func (p *commandProgress) status(format string, args ...any) {
	if p == nil {
		return
	}
	if p.activity != nil {
		p.activity.Clear()
		p.activity = nil
	}
	currentCLIReporter().Status(fmt.Sprintf(format, args...))
}

func elapsedSince(start time.Time) string {
	return time.Since(start).Round(time.Millisecond).String()
}

const (
	defaultActivityDelay    = 200 * time.Millisecond
	defaultActivityInterval = 100 * time.Millisecond
)

var activityFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// CLIOutputFlags are the output controls shared by gestaltd commands. They
// describe human-facing output only; machine-readable command output remains
// owned by the command and should stay on stdout.
type CLIOutputFlags struct {
	Verbose    bool
	Quiet      bool
	NoProgress bool
}

// parseGlobalCLIOutputFlags removes shared output flags from the command line.
// Short -v is deliberately not recognized here: root -v is the legacy version
// flag, while command-local sync -v remains a sync flag.
func parseGlobalCLIOutputFlags(args []string) (CLIOutputFlags, []string, error) {
	var flags CLIOutputFlags
	filtered := make([]string, 0, len(args))
	endOfFlags := false
	pendingValue := false
	command := ""
	for _, arg := range args {
		if endOfFlags {
			filtered = append(filtered, arg)
			continue
		}
		if pendingValue {
			filtered = append(filtered, arg)
			pendingValue = false
			continue
		}
		if arg == "--" {
			endOfFlags = true
			filtered = append(filtered, arg)
			continue
		}
		if isCLIValueFlag(arg) {
			filtered = append(filtered, arg)
			if !strings.Contains(arg, "=") {
				pendingValue = true
			}
			continue
		}
		if !strings.HasPrefix(arg, "-") && command == "" {
			command = arg
		}
		if name, rawValue, matched := parseCLIOutputBool(arg); matched {
			if command == "sync" && name == "verbose" {
				filtered = append(filtered, arg)
				continue
			}
			value, err := parseCLIOutputBoolValue(name, rawValue)
			if err != nil {
				return flags, nil, err
			}
			switch name {
			case "verbose":
				flags.Verbose = value
			case "quiet":
				flags.Quiet = value
			case "no-progress":
				flags.NoProgress = value
			}
			continue
		}
		filtered = append(filtered, arg)
	}
	return flags, filtered, nil
}

func parseCLIOutputBool(arg string) (name, rawValue string, matched bool) {
	for _, name := range []string{"verbose", "quiet", "no-progress"} {
		for _, prefix := range []string{"--", "-"} {
			flagName := prefix + name
			switch {
			case arg == flagName:
				return name, "true", true
			case strings.HasPrefix(arg, flagName+"="):
				return name, strings.TrimPrefix(arg, flagName+"="), true
			}
		}
	}
	return "", "", false
}

func parseCLIOutputBoolValue(name, rawValue string) (bool, error) {
	switch rawValue {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("invalid value %q for --%s; expected true or false", rawValue, name)
	}
}

// isCLIValueFlag lists flag names used by the daemon command tree whose next
// token is a value. Keeping this list explicit lets a dash-prefixed value pass
// through without treating it as a shared output flag, while boolean flags
// such as --locked still allow a following global flag.
func isCLIValueFlag(arg string) bool {
	if strings.Contains(arg, "=") {
		return false
	}
	var name string
	switch {
	case strings.HasPrefix(arg, "--"):
		name = strings.TrimPrefix(arg, "--")
	case strings.HasPrefix(arg, "-"):
		name = strings.TrimPrefix(arg, "-")
	default:
		return false
	}
	if name == "" || strings.HasPrefix(name, "-") {
		return false
	}
	switch name {
	case "app", "artifacts-dir", "bucket", "cache-dir", "config", "dist-dir", "format", "harness", "kind", "lockfile", "manifest", "name", "output", "output-format", "parallelism", "path", "platform", "port", "provider", "ref", "remote", "remote-token", "repo", "set", "version":
		return true
	default:
		return false
	}
}

// CLIOutputPolicy controls a TerminalReporter. Interactive output is always
// directed to the configured writer, which is stderr for the process-level
// reporter. The reporter never changes slog's default logger.
type CLIOutputPolicy struct {
	CLIOutputFlags
	Interactive      bool
	ActivityDelay    time.Duration
	ActivityInterval time.Duration
}

var (
	cliReporterMu sync.RWMutex
	cliReporter   *TerminalReporter
	cliRunMu      sync.Mutex
)

type cliReporterScope struct {
	previous *TerminalReporter
}

// installCLIReporter swaps the process reporter for tests and nested setup.
func installCLIReporter(reporter *TerminalReporter) *cliReporterScope {
	cliReporterMu.Lock()
	defer cliReporterMu.Unlock()
	scope := &cliReporterScope{previous: cliReporter}
	cliReporter = reporter
	return scope
}

func (s *cliReporterScope) Close() {
	cliReporterMu.Lock()
	defer cliReporterMu.Unlock()
	cliReporter = s.previous
}

// currentCLIReporter returns the active process reporter.
func currentCLIReporter() *TerminalReporter {
	cliReporterMu.RLock()
	reporter := cliReporter
	cliReporterMu.RUnlock()
	if reporter != nil {
		return reporter
	}

	cliReporterMu.Lock()
	defer cliReporterMu.Unlock()
	if cliReporter == nil {
		cliReporter = NewTerminalReporter(io.Discard, CLIOutputPolicy{})
	}
	return cliReporter
}

func currentCLIOutputFlags() CLIOutputFlags {
	return currentCLIReporter().policy.CLIOutputFlags
}

func (p CLIOutputPolicy) withDefaults() CLIOutputPolicy {
	if p.ActivityDelay <= 0 {
		p.ActivityDelay = defaultActivityDelay
	}
	if p.ActivityInterval <= 0 {
		p.ActivityInterval = defaultActivityInterval
	}
	return p
}

// TerminalReporter renders human-facing status to one writer. It is safe for
// activity timer callbacks and command cleanup to call concurrently.
type TerminalReporter struct {
	mu     sync.Mutex
	output io.Writer
	policy CLIOutputPolicy
	active *activityState
}

type activityState struct {
	message string
	stop    chan struct{}
	timer   *time.Timer
	frame   int
	started bool
	done    bool
}

type TerminalActivity struct {
	reporter *TerminalReporter
	state    *activityState
	once     sync.Once
	started  time.Time
}

// NewTerminalReporter creates a reporter writing to output. Callers should
// pass os.Stderr for process output and set Interactive from the destination's
// terminal state.
func NewTerminalReporter(output io.Writer, policy CLIOutputPolicy) *TerminalReporter {
	if output == nil {
		output = io.Discard
	}
	return &TerminalReporter{output: output, policy: policy.withDefaults()}
}

func (r *TerminalReporter) Start(message string) *TerminalActivity {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.active != nil {
		r.finishLocked(r.active, "")
	}
	now := time.Now()
	state := &activityState{
		message: strings.TrimSpace(message),
		stop:    make(chan struct{}),
	}
	r.active = state
	activity := &TerminalActivity{reporter: r, state: state, started: now}
	if r.policy.Quiet {
		return activity
	}
	if !r.policy.Interactive || r.policy.NoProgress {
		r.writeStableLocked(state.message)
		state.started = true
		return activity
	}
	state.timer = time.AfterFunc(r.policy.ActivityDelay, func() {
		r.activate(state)
	})
	return activity
}

func (r *TerminalReporter) activate(state *activityState) {
	r.mu.Lock()
	if r.active != state || state.done || r.policy.Quiet {
		r.mu.Unlock()
		return
	}
	state.started = true
	r.writeActivityLocked(state)
	r.mu.Unlock()

	ticker := time.NewTicker(r.policy.ActivityInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.mu.Lock()
			if r.active != state || state.done {
				r.mu.Unlock()
				return
			}
			r.writeActivityLocked(state)
			r.mu.Unlock()
		case <-state.stop:
			return
		}
	}
}

func (r *TerminalReporter) Status(message string) {
	r.writeNormal(message)
}

func (r *TerminalReporter) Verbose(message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.policy.Verbose && !r.policy.Quiet {
		r.clearActiveLocked()
		r.writeStableLocked(message)
	}
}

func (r *TerminalReporter) Warning(message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearActiveLocked()
	r.writeStableLocked("warning: " + strings.TrimSpace(message))
}

func (r *TerminalReporter) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearActiveLocked()
}

func (r *TerminalReporter) writeNormal(message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.policy.Quiet {
		return
	}
	r.clearActiveLocked()
	r.writeStableLocked(message)
}

func (r *TerminalReporter) finish(state *activityState, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active != state || state.done {
		return
	}
	r.finishLocked(state, message)
}

func (r *TerminalReporter) finishLocked(state *activityState, message string) {
	state.done = true
	if state.timer != nil {
		state.timer.Stop()
	}
	close(state.stop)
	if state.started && r.policy.Interactive && !r.policy.NoProgress && !r.policy.Quiet {
		r.clearLineLocked(state.message)
	}
	r.active = nil
	if !r.policy.Quiet && strings.TrimSpace(message) != "" {
		r.writeStableLocked(message)
	}
}

func (r *TerminalReporter) clearActiveLocked() {
	if r.active == nil {
		return
	}
	r.finishLocked(r.active, "")
}

func (r *TerminalReporter) writeActivityLocked(state *activityState) {
	_, _ = io.WriteString(r.output, renderActivityFrame(state.frame, state.message))
	state.frame++
}

func (r *TerminalReporter) writeStableLocked(message string) {
	_, _ = io.WriteString(r.output, strings.TrimSpace(message)+"\n")
}

func (r *TerminalReporter) clearLineLocked(message string) {
	_, _ = io.WriteString(r.output, renderClearLine(message))
}

func (a *TerminalActivity) Finish(message string) {
	a.once.Do(func() { a.reporter.finish(a.state, message) })
}

func (a *TerminalActivity) Clear() {
	a.once.Do(func() { a.reporter.finish(a.state, "") })
}

func renderActivityFrame(frame int, message string) string {
	return "\r" + activityFrames[frame%len(activityFrames)] + " " + strings.TrimSpace(message)
}

func renderClearLine(message string) string {
	return "\r" + strings.Repeat(" ", len(renderActivityFrame(0, message))) + "\r"
}
