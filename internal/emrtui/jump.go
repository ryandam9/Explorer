package emrtui

import (
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// s3JumpDoneMsg is delivered after the suspended s3 TUI exits.
type s3JumpDoneMsg struct{ err error }

// s3LogTarget derives the S3 bucket and prefix that hold a cluster's (or a
// specific step's) logs, from the cluster's LogUri. EMR archives logs under
// <LogUri>/<cluster-id>/, with per-step logs under
// <LogUri>/<cluster-id>/steps/<step-id>/. Pure, so it is table-tested. ok is
// false when the cluster has no log URI configured (logging disabled).
func s3LogTarget(logURI, clusterID, stepID string) (bucket, prefix string, ok bool) {
	logURI = strings.TrimSpace(logURI)
	if logURI == "" || clusterID == "" {
		return "", "", false
	}
	rest := strings.TrimPrefix(logURI, "s3://")
	rest = strings.TrimPrefix(rest, "s3n://")
	rest = strings.TrimPrefix(rest, "s3a://")
	bucket, base, _ := strings.Cut(rest, "/")
	if bucket == "" {
		return "", "", false
	}
	base = strings.Trim(base, "/")

	var b strings.Builder
	if base != "" {
		b.WriteString(base)
		b.WriteString("/")
	}
	b.WriteString(clusterID)
	b.WriteString("/")
	if stepID != "" {
		b.WriteString("steps/")
		b.WriteString(stepID)
		b.WriteString("/")
	}
	return bucket, b.String(), true
}

// s3JumpArgs builds the argv for the child `s3` invocation that opens an EMR log
// location. Pure, so it is table-tested.
func s3JumpArgs(bucket, prefix, region, profile, configPath string) []string {
	args := []string{"s3", "--bucket", bucket}
	if prefix != "" {
		args = append(args, "--prefix", prefix)
	}
	if region != "" && region != "global" {
		args = append(args, "--region", region)
	}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	return args
}

// jumpToS3LogsCmd suspends the dashboard and runs the s3 TUI as a child of this
// same binary, rooted at the log location for the given cluster/step (AXE-036).
func (mm *m) jumpToS3LogsCmd(bucket, prefix, region string) tea.Cmd {
	self, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return s3JumpDoneMsg{err: err} }
	}
	var profile string
	if mm.appCfg != nil {
		profile = mm.appCfg.AWS.Profile
	}
	args := s3JumpArgs(bucket, prefix, region, profile, mm.configPath)
	return tea.ExecProcess(exec.Command(self, args...), func(err error) tea.Msg {
		return s3JumpDoneMsg{err: err}
	})
}

// jumpToClusterLogs opens the S3 browser at a cluster's log root, or toasts when
// the cluster has no log destination configured.
func (mm *m) jumpToClusterLogs(cl Cluster, cmds *[]tea.Cmd) {
	bucket, prefix, ok := s3LogTarget(cl.LogURI, cl.ID, "")
	if !ok {
		mm.setToast("No log destination configured for this cluster")
		*cmds = append(*cmds, toastCmd(3*time.Second))
		return
	}
	*cmds = append(*cmds, mm.jumpToS3LogsCmd(bucket, prefix, cl.Region))
}

// jumpToStepLogs opens the S3 browser at a step's log folder under its cluster.
func (mm *m) jumpToStepLogs(s Step, cmds *[]tea.Cmd) {
	bucket, prefix, ok := s3LogTarget(mm.stepsCluster.LogURI, mm.stepsCluster.ID, s.ID)
	if !ok {
		mm.setToast("No log destination configured for this cluster")
		*cmds = append(*cmds, toastCmd(3*time.Second))
		return
	}
	*cmds = append(*cmds, mm.jumpToS3LogsCmd(bucket, prefix, mm.stepsCluster.Region))
}
