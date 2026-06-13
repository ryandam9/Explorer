package ecstriage

import (
	"strings"
	"testing"
	"time"
)

func i32(v int32) *int32 { return &v }

func TestClassify_ExitCodeAnnotations(t *testing.T) {
	tests := []struct {
		name     string
		exitCode *int32
		reason   string
		wantNote string
	}{
		{"OOM-kill 137", i32(137), "", "possible OOM-kill (137 = 128+9, SIGKILL)"},
		{"segfault 139", i32(139), "", "segfault (139 = 128+11, SIGSEGV)"},
		{"SIGTERM 143", i32(143), "", "SIGTERM (143 = 128+15) — stopped by a signal, often a normal shutdown"},
		{"SIGABRT 134", i32(134), "", "SIGABRT (134 = 128+6) — likely an abort()/assert"},
		{"general error 1", i32(1), "", "general application error"},
		{"clean exit 0", i32(0), "", "exited cleanly"},
		{"explicit OOM reason beats exit code", i32(1), "OutOfMemoryError: container killed", "out-of-memory: container exceeded its memory limit — raise memory or fix the leak"},
		{"unknown code falls back to reason", i32(42), "CannotPullContainerError", "CannotPullContainerError"},
		{"nil exit code uses reason", nil, "CannotPullContainerError: pull rate limit", "CannotPullContainerError: pull rate limit"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tasks := []Task{{
				ARN:           "arn:aws:ecs:us-east-1:111122223333:task/my-cluster/3f9aabcdef",
				Cluster:       "my-cluster",
				Region:        "us-east-1",
				StoppedReason: "Essential container in task exited",
				Containers:    []Container{{Name: "app", ExitCode: tc.exitCode, Reason: tc.reason}},
			}}
			got := Classify(tasks)
			if len(got) != 1 {
				t.Fatalf("want 1 record, got %d", len(got))
			}
			if got[0].ExitNote != tc.wantNote {
				t.Errorf("ExitNote = %q, want %q", got[0].ExitNote, tc.wantNote)
			}
			if got[0].Task != "3f9aabcdef" {
				t.Errorf("Task = %q, want short ID 3f9aabcdef", got[0].Task)
			}
		})
	}
}

func TestClassify_CulpritSelection(t *testing.T) {
	// The non-zero-exit container should be chosen over a clean sidecar.
	task := Task{
		ARN:           "task/c/abc",
		StoppedReason: "Essential container in task exited",
		Containers: []Container{
			{Name: "sidecar", ExitCode: i32(0)},
			{Name: "app", ExitCode: i32(137)},
		},
	}
	got := Classify([]Task{task})
	if got[0].Container != "app" {
		t.Errorf("culprit container = %q, want app", got[0].Container)
	}
	if got[0].ExitCode == nil || *got[0].ExitCode != 137 {
		t.Errorf("ExitCode = %v, want 137", got[0].ExitCode)
	}
}

func TestClassify_NoReasonGetsPlaceholder(t *testing.T) {
	got := Classify([]Task{{ARN: "task/c/x"}})
	if got[0].Reason != "(no reason reported)" {
		t.Errorf("Reason = %q, want placeholder", got[0].Reason)
	}
	if got[0].Container != "" {
		t.Errorf("Container = %q, want empty (no containers)", got[0].Container)
	}
}

func TestClassify_SortsNewestFirst(t *testing.T) {
	t1 := time.Date(2026, 6, 12, 1, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 12, 2, 0, 0, 0, time.UTC)
	got := Classify([]Task{
		{ARN: "task/c/old", StoppedAt: t1},
		{ARN: "task/c/new", StoppedAt: t2},
	})
	if got[0].Task != "new" || got[1].Task != "old" {
		t.Errorf("order = [%s, %s], want [new, old]", got[0].Task, got[1].Task)
	}
}

func TestExitDisplay(t *testing.T) {
	cases := []struct {
		rec  Record
		want string
	}{
		{Record{ExitCode: i32(137), ExitNote: "possible OOM-kill"}, "137 (possible OOM-kill)"},
		{Record{ExitCode: i32(2)}, "2"},
		{Record{ExitNote: "CannotPullContainerError"}, "CannotPullContainerError"},
		{Record{}, "-"},
	}
	for _, c := range cases {
		if got := c.rec.ExitDisplay(); got != c.want {
			t.Errorf("ExitDisplay() = %q, want %q", got, c.want)
		}
	}
}

func TestShortTaskID(t *testing.T) {
	cases := map[string]string{
		"arn:aws:ecs:us-east-1:111122223333:task/cluster/abc123": "abc123",
		"abc123": "abc123",
		"arn:aws:ecs:us-east-1:111122223333:task/abc123": "abc123",
	}
	for in, want := range cases {
		if got := shortTaskID(in); got != want {
			t.Errorf("shortTaskID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRender_JSONAndTable(t *testing.T) {
	recs := Classify([]Task{{
		ARN:           "task/my-cluster/3f9a",
		Cluster:       "my-cluster",
		Region:        "us-east-1",
		StoppedReason: "Essential container in task exited",
		StoppedAt:     time.Date(2026, 6, 12, 1, 14, 0, 0, time.UTC),
		Containers:    []Container{{Name: "app", ExitCode: i32(137)}},
	}})

	var sb strings.Builder
	if err := Render(&sb, recs, "table", false); err != nil {
		t.Fatalf("table render: %v", err)
	}
	out := sb.String()
	for _, want := range []string{"my-cluster", "3f9a", "OOM-kill", "2026-06-12 01:14"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}

	sb.Reset()
	if err := Render(&sb, recs, "json", false); err != nil {
		t.Fatalf("json render: %v", err)
	}
	if !strings.Contains(sb.String(), `"exit_code": 137`) {
		t.Errorf("json missing exit_code:\n%s", sb.String())
	}
}
