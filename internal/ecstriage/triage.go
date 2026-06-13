// Package ecstriage implements the ECS stopped-task triage report (AXE-015):
// "why did my task stop?" answered without spelunking through the console.
// For each recently stopped task it surfaces the task-level stop reason and
// the failing container's exit code, with the exit code translated into the
// usual suspects — 137 is almost always an OOM-kill, 139 a segfault — so the
// answer is in the table rather than a man page.
//
// All analysis is pure: collect.go fetches the stopped tasks, Classify here
// turns them into Records. ECS retains stopped tasks for roughly an hour, so
// an empty report means "nothing stopped recently", not "nothing ever fails".
package ecstriage

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Container is one container within a stopped task.
type Container struct {
	Name     string
	ExitCode *int32 // nil when ECS never recorded one (e.g. failed to start)
	Reason   string // human-readable container-level reason, when present
}

// Task is a stopped ECS task, as gathered by collect.go (or hand-built in
// tests).
type Task struct {
	ARN           string
	Cluster       string // cluster name (not ARN)
	Region        string
	Group         string    // task group, e.g. "service:my-svc"
	StopCode      string    // ECS StopCode, e.g. "EssentialContainerExited"
	StoppedReason string    // task-level stoppedReason
	StoppedAt     time.Time // zero when unknown
	Containers    []Container
}

// Record is one triaged stopped task: a single row in the report.
type Record struct {
	StoppedAt time.Time `json:"stopped_at"`
	Task      string    `json:"task"`    // short task ID (ARN tail)
	Cluster   string    `json:"cluster"` // cluster name
	Region    string    `json:"region"`
	Group     string    `json:"group,omitempty"`
	StopCode  string    `json:"stop_code,omitempty"`
	Reason    string    `json:"reason"`              // task-level stop reason
	Container string    `json:"container,omitempty"` // the failing container's name
	ExitCode  *int32    `json:"exit_code,omitempty"` // the failing container's exit code
	ExitNote  string    `json:"exit_note,omitempty"` // plain-English gloss on the exit code
}

// Classify turns stopped tasks into report records, newest first. It is the
// pure heart of the feature and is exercised by fixtures.
func Classify(tasks []Task) []Record {
	out := make([]Record, 0, len(tasks))
	for _, t := range tasks {
		rec := Record{
			StoppedAt: t.StoppedAt,
			Task:      shortTaskID(t.ARN),
			Cluster:   t.Cluster,
			Region:    t.Region,
			Group:     t.Group,
			StopCode:  t.StopCode,
			Reason:    strings.TrimSpace(t.StoppedReason),
		}
		if c, ok := culprit(t.Containers); ok {
			rec.Container = c.Name
			rec.ExitCode = c.ExitCode
			rec.ExitNote = exitNote(c)
		}
		if rec.Reason == "" {
			rec.Reason = "(no reason reported)"
		}
		out = append(out, rec)
	}
	Sort(out)
	return out
}

// culprit picks the container most likely to explain the stop: the first with
// a non-zero exit code, else the first with a container-level reason, else the
// first container. Returns false when there are no containers at all.
func culprit(cs []Container) (Container, bool) {
	if len(cs) == 0 {
		return Container{}, false
	}
	for _, c := range cs {
		if c.ExitCode != nil && *c.ExitCode != 0 {
			return c, true
		}
	}
	for _, c := range cs {
		if strings.TrimSpace(c.Reason) != "" {
			return c, true
		}
	}
	return cs[0], true
}

// exitNote glosses a container's exit code and reason in plain English. An
// explicit container reason mentioning memory is the strongest OOM signal;
// otherwise we fall back to the conventional 128+signal exit codes. We
// deliberately hedge ("possible") rather than assert, since the same code can
// have benign causes.
func exitNote(c Container) string {
	if oom := mentionsOOM(c.Reason); oom != "" {
		return oom
	}
	if c.ExitCode == nil {
		if r := strings.TrimSpace(c.Reason); r != "" {
			return r
		}
		return ""
	}
	switch *c.ExitCode {
	case 0:
		return "exited cleanly"
	case 1:
		return "general application error"
	case 134:
		return "SIGABRT (134 = 128+6) — likely an abort()/assert"
	case 137:
		return "possible OOM-kill (137 = 128+9, SIGKILL)"
	case 139:
		return "segfault (139 = 128+11, SIGSEGV)"
	case 143:
		return "SIGTERM (143 = 128+15) — stopped by a signal, often a normal shutdown"
	default:
		if r := strings.TrimSpace(c.Reason); r != "" {
			return r
		}
		return ""
	}
}

// mentionsOOM returns an OOM note when the container reason indicates the
// kernel/agent killed it for memory, "" otherwise.
func mentionsOOM(reason string) string {
	r := strings.ToLower(reason)
	if strings.Contains(r, "outofmemory") || strings.Contains(r, "out of memory") ||
		strings.Contains(r, "oomkill") || strings.Contains(r, "memory limit") {
		return "out-of-memory: container exceeded its memory limit — raise memory or fix the leak"
	}
	return ""
}

// shortTaskID reduces a task ARN to its trailing ID. Plain IDs pass through.
func shortTaskID(arn string) string {
	if i := strings.LastIndexByte(arn, '/'); i >= 0 && i+1 < len(arn) {
		return arn[i+1:]
	}
	if i := strings.LastIndexByte(arn, ':'); i >= 0 && i+1 < len(arn) {
		return arn[i+1:]
	}
	return arn
}

// Sort orders records newest-stopped first, with stable region/cluster/task
// tie-breaks so output is deterministic.
func Sort(recs []Record) {
	sort.SliceStable(recs, func(i, j int) bool {
		a, b := recs[i], recs[j]
		if !a.StoppedAt.Equal(b.StoppedAt) {
			return a.StoppedAt.After(b.StoppedAt)
		}
		if a.Region != b.Region {
			return a.Region < b.Region
		}
		if a.Cluster != b.Cluster {
			return a.Cluster < b.Cluster
		}
		return a.Task < b.Task
	})
}

// ExitDisplay renders the exit code (and its note) for a single column.
func (r Record) ExitDisplay() string {
	if r.ExitCode == nil {
		if r.ExitNote != "" {
			return r.ExitNote
		}
		return "-"
	}
	if r.ExitNote != "" {
		return fmt.Sprintf("%d (%s)", *r.ExitCode, r.ExitNote)
	}
	return fmt.Sprintf("%d", *r.ExitCode)
}
