Configuration is **optional**. With no config file, AWS Explorer runs on
built-in defaults. When you want to pin services, regions, output columns or a
theme, write a `config.yaml`.

## Where the config is found

The search order is:

1. The `--config <path>` flag, if given.
2. `./config.yaml` in the current directory.
3. The per-user config directory (`~/.config/aws_explorer/config.yaml` on
   Linux, the equivalent on macOS/Windows).
4. The built-in defaults embedded in the binary.

Run `aws_explorer config init` to write a starter file, and
`aws_explorer config show` / `config path` to inspect the active settings.
See the [config command reference](config.md).

## Key sections

```yaml
app:
  defaultoutput: table      # table | json | ndjson | csv
  defaultmode: cli
  timeoutseconds: 30        # per service/region collection timeout
  maxconcurrency: 8         # bounded goroutine pool for parallel collection
  downloaddir: ""           # where the S3 browser saves downloads

aws:
  profile: default
  authmethod: auto          # see the Authentication guide
  regions:
    - us-east-1
  allregions: false
  retry:
    maxattempts: 0          # 0 = SDK default (3); raise for throttled accounts
    mode: ""                # "" = standard; "adaptive" adds client-side rate limiting

services:
  ec2:   { enabled: true }
  s3:    { enabled: true }
  # …one entry per service key; set enabled:false to skip a collector

ui:
  theme: princess-parrot    # one of the 12 built-in themes
```

## Resilient scanning

Collection is best-effort and concurrent. When a service/region fails partway
through — a later page throttles, or a per-item call is denied — everything
collected before the failure is kept and flagged *partial* rather than
discarded. For large, throttled accounts (`RequestLimitExceeded`), tune
`aws.retry`:

```yaml
aws:
  retry:
    maxattempts: 8       # keep retrying longer than the default 3 attempts
    mode: adaptive       # client-side rate limiting that backs off automatically
```

`adaptive` mode is usually the right choice for `--all-regions` sweeps of busy
accounts.

## Themes

The TUIs ship with 12 color themes, all named after Australian birds (the
default is `princess-parrot`). Set the active theme under `ui.theme`, or pass
`--theme` on the TUIs that accept it (`s3`, `cw`, `vpc`). Each theme exposes 24
granular color *roles* (heading, table header, status bar, alerts, …) that you
can override individually; unset roles fall back to a sensible related role.
Themes and colors are also editable live from the **settings panel** (`S`) in
the summary and VPC TUIs.

| Theme | Palette feel |
|-------|--------------|
| `princess-parrot` | Green, blue and pink (default) |
| `spotted-pardalote` | Warm yellow, orange and red |
| `plains-wanderer` | Cream, tan and golden brown |
| `bee-eater` | Cyan, blue and amber |
| `rose-crowned-fruit-dove` | Magenta, coral and green |
| `eastern-rosella` | Yellow, lime and red |
| `oriole` | Gold, salmon and lavender |
| `superb-fairy-wren` | Rust, tan and cream |
| `cassowary` | Teal, gold and pink |
| `yellow-robin` | Bright yellow, slate and amber |
| `galah` | Pink, blush and slate |
| `blue-winged-kookaburra` | Light cyan, teal and orange |

## Customising displayed columns

For the typed services you can override which summary columns and detail
fields show, per resource type, under `services.<key>.resources`. This applies
in the CLI tables, the summary TUI and the VPC explorer alike.
