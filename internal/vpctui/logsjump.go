package vpctui

import (
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ryandam9/aws_explorer/internal/loggroup"
)

// cwJumpDoneMsg is delivered after the suspended CloudWatch Logs TUI exits,
// carrying any error from launching or running the child process.
type cwJumpDoneMsg struct{ err error }

// jumpToLogsForSelected opens the CloudWatch Logs explorer for the resource
// currently selected in the resource table, when a log group can be derived
// from its identity (Lambda, RDS). It returns the command to run plus an empty
// reason on success, or a nil command and a human-readable reason the caller
// can surface in the status bar when no jump is possible.
func (m *Model) jumpToLogsForSelected() (tea.Cmd, string) {
	maps := m.resourceView
	idx := m.resourceTable.Cursor()
	if idx < 0 || idx >= len(maps) {
		return nil, ""
	}
	row := maps[idx]

	var service string
	switch m.activeResource {
	case rtLambda:
		service = "lambda"
	case rtRDS:
		service = "rds"
	default:
		return nil, "Logs: select a Lambda function or RDS instance first"
	}

	group, ok := loggroup.For(loggroup.Resource{
		Service: service,
		Name:    row["name"],
		ID:      firstID(row),
	})
	if !ok {
		return nil, "No CloudWatch log group derivable for this resource"
	}

	region := ""
	if m.selectedVPC != nil {
		region = m.selectedVPC.Region
	}
	return m.jumpToLogsCmd(region, group), ""
}

// jumpToLogsCmd suspends the VPC TUI and runs the CloudWatch Logs TUI as a
// child of this same binary, pre-filtered to group in region. tea.ExecProcess
// hands the terminal to the child and restores it on exit, so quitting the log
// view returns to the VPC explorer with its state intact. Credentials follow
// from the same --profile/--config this TUI is using.
func (m *Model) jumpToLogsCmd(region, group string) tea.Cmd {
	self, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return cwJumpDoneMsg{err: err} }
	}
	args := []string{"cw", "--group", group}
	if region != "" && region != "global" {
		args = append(args, "--region", region)
	}
	if m.awsCfg != nil && m.awsCfg.Profile != "" {
		args = append(args, "--profile", m.awsCfg.Profile)
	}
	if m.configPath != "" {
		args = append(args, "--config", m.configPath)
	}
	return tea.ExecProcess(exec.Command(self, args...), func(err error) tea.Msg {
		return cwJumpDoneMsg{err: err}
	})
}
