# ECS Stopped-Task Triage

`ecs stopped` answers the perennial "why did my task stop?" ticket without
spelunking through the console. For each recently stopped task it prints the
task-level stop reason and the failing container's exit code, with the exit
code translated into plain English.

```bash
./bin/aws_explorer ecs stopped [--cluster <name-or-arn>] [--all-regions] [-o table|json|ndjson|csv]
```

```
SNO  STOPPED AT (UTC)  CLUSTER     TASK    REASON                              CONTAINER  EXIT
1    2026-06-12 01:14  prod        3f9a…   Essential container in task exited  app        137 (possible OOM-kill (137 = 128+9, SIGKILL))
2    2026-06-12 01:10  prod        77b2…   Task failed to start                web        CannotPullContainerError: pull rate limit
```

Exit-code glosses (hedged — the same code can have benign causes):

| Signal | Exit code | Note |
|--------|-----------|------|
| SIGKILL | `137` (128+9) | possible OOM-kill — raise memory or fix the leak |
| SIGSEGV | `139` (128+11) | segfault |
| SIGABRT | `134` (128+6) | likely an `abort()`/assert |
| SIGTERM | `143` (128+15) | stopped by a signal, often a normal shutdown |

A container reason mentioning memory (`OutOfMemoryError`, `memory limit`) is
treated as a stronger OOM signal than the exit code alone.

| Flag | Default | Description |
|------|---------|-------------|
| `--cluster` | _(all)_ | Limit to one cluster (name or ARN); default scans every cluster in scope |
| `--output` / `-o` | `table` | `table`, `json`, `ndjson`, `csv` |

> **About the retention window.** ECS keeps stopped tasks for roughly one
> hour. An empty report means nothing stopped in that window, not that nothing
> ever fails — run it soon after the failure.

**IAM permissions.** Read-only: `ecs:ListClusters`, `ecs:ListTasks`,
`ecs:DescribeTasks`. Any denial skips that region with a note on stderr and
never aborts the run.
