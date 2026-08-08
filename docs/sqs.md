# SQS Queue Explorer TUI

An interactive explorer for SQS queues: attributes, tags, dead-letter
relationships, CloudWatch metric sparklines, an opt-in message peek, and a
jump into the CloudWatch Logs explorer for a queue's Lambda consumers.

```bash
./bin/aws_explorer sqs [flags]
```

The global `--profile`, `--auth-method`, `--role-arn`, `--region` and
`--all-regions` flags apply: `--region` pins a single region, `--all-regions`
sweeps every enabled region and shows each queue's region in the sidebar, and
otherwise the config's `aws.regions` list is used.

| Flag | Default | Description |
|------|---------|-------------|
| `--queue` / `-q` | — | Initial queue name filter (also applied server-side as a `ListQueues` name prefix) |
| `--theme` | `spotted-pardalote` | UI theme name |

```bash
# Browse queues in one region
./bin/aws_explorer sqs --region us-east-1

# Pre-filter to queues whose name starts with "orders"
./bin/aws_explorer sqs -q orders
```

The sidebar lists every queue your credentials can see across the selected
regions (a region that denies `sqs:ListQueues` degrades to a logged warning,
never an abort). In single-region mode the sidebar shows each visited queue's
approximate depth; a blank means "not fetched yet", never zero.

### Queue overview

Selecting a queue loads (and caches) its `GetQueueAttributes`,
`ListQueueTags` and `ListDeadLetterSourceQueues` — three independent
best-effort calls, so a denied tag read can't hide the attributes. Counts are
labelled `~` (SQS counts are approximate by design) and any failed read shows
as an explicit error line with `r` to retry — never as an empty value.

| Key | Action |
|-----|--------|
| `↑`/`↓` | Navigate queues (details load eagerly, cached per queue) |
| `/` | Filter queues by name or region |
| `P` | Peek at the queue's messages (see below) |
| `d` | Jump to the queue's dead-letter queue (redrive target) |
| `m` | Toggle CloudWatch metric sparklines |
| `L` | Open the CloudWatch Logs explorer for the queue's Lambda consumer |
| `o` | Copy the console URL (opens the browser when local) |
| `y` | Copy the queue URL |
| `r` / `R` | Refresh the selected queue's detail / reload the queue list |

The overview also shows the reverse redrive relationship — which queues use
the selected queue as *their* DLQ — so both directions of the dead-letter
graph are one keypress apart.

### Message peek (`P`) — read-only, with one honest caveat

SQS has no truly non-destructive read, so the peek is **opt-in behind a
confirmation** that states exactly what it does:

- Samples up to **50 visible messages** (`ReceiveMessage`, batches of 10).
  It is a *sample*, not the queue: SQS returns messages from a random subset
  of servers, and order is not guaranteed. The view labels it as such.
- Messages are **not deleted** and visibility is returned immediately
  (`VisibilityTimeout=0`), so consumers are not starved.
- SQS still **increments each sampled message's receive count** — on a queue
  with a redrive policy, repeated peeks move messages closer to the DLQ. The
  confirmation shows the queue's actual `maxReceiveCount` when one is set,
  and warns about message-group holds on FIFO queues.

The tool never calls `DeleteMessage`, `PurgeQueue` or `SendMessage`.

| Key | Action |
|-----|--------|
| `↑`/`↓`, `PgUp`/`PgDn` | Navigate the sampled messages (zebra-striped shared table) |
| `v` / `Enter` | Record view: the message's full body (JSON pretty-printed) and every attribute, unclipped |
| `y` | Copy the selected message's body |
| `s` | Export the sampled messages to the downloads directory |
| `P` | Re-peek (another sample — confirmation shown again) |
| `Esc` | Back to the queue overview |

Body cells clip at 160 characters with an ellipsis; the record view (`v`) is
the escape hatch with the complete body. Message bodies are never written to
the tool's logs.

### Metrics (`m`)

Sparklines for the last 3 hours — visible depth and age-of-oldest (Maximum
statistic: capacity questions need peak, not average) plus sent/received
rates (Sum) — fetched in **one** batched `GetMetricData` call. `GetMetricData`
is a paid API, so refreshes are floored to once per minute and the fetch time
is shown; "no data" renders as *no data*, never as a flat zero line.

### Consumer logs (`L`)

`L` looks up Lambda event source mappings whose source is the queue and hands
the terminal to the CloudWatch Logs TUI pre-filtered to that function's log
group; quitting it returns here with state intact. Only Lambda consumers are
discoverable this way — the tool says "no Lambda consumers found" rather than
implying nothing consumes the queue.

Press `?` anywhere for the full key reference; the status bar shows only the
keys usable right now (eliding on narrow terminals).
