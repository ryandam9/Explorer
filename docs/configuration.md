# Configuration

Configuration is **optional** — the binary embeds the default config and runs
from any directory with zero setup. When a `config.yaml` exists it is
discovered in this order:

1. The `--config` flag
2. `./config.yaml` in the current directory
3. The user config directory (`~/.config/aws_explorer/config.yaml` on Linux,
   `~/Library/Application Support/aws_explorer/config.yaml` on macOS)
4. The built-in defaults embedded in the binary

CLI flags override config file values at runtime.

```bash
# Scaffold a starter config in the current directory
aws_explorer config init

# Scaffold the per-user config used from any directory
aws_explorer config init --path ~/.config/aws_explorer/config.yaml

# Show which config file is active
aws_explorer config path
```

Theme edits saved from the in-app settings panel (`Ctrl+S`) are written to the
active config file; when running on built-in defaults the file is created in
the user config directory on first save.

### Full Configuration Reference

```yaml
app:
  defaultOutput: table        # table | json
  defaultMode: cli            # cli | tui
  timeoutSeconds: 30          # per-collector timeout
  maxConcurrency: 8           # max parallel collectors
  downloadDir: ""             # S3 browser download target ("D"); ~ expands to home,
                              # empty = current dir; created automatically if missing
  previewMaxSize: ""          # S3 "p" preview read cap: "10MB"/"512KB"/bytes;
                              # empty = 10MB default, clamped to 4KB–64MB

aws:
  # Auth method: auto | profile | env | static | sts
  authMethod: auto
  profile: default

  # STS AssumeRole (used when authMethod: sts)
  sts:
    roleArn: ""               # required: arn:aws:iam::123456789012:role/MyRole
    sessionName: ""           # optional; defaults to "aws-explorer"
    externalId: ""            # optional; for cross-account trust policies
    mfaSerial: ""             # optional; ARN of your MFA device
    durationSeconds: 0        # optional; 0 = AWS default (3600s)

  # Static credentials (used when authMethod: static)
  static:
    accessKeyId: ""
    secretAccessKey: ""
    sessionToken: ""          # optional; for temporary credentials

  # Retry tuning for every AWS API call (applies to all auth methods)
  retry:
    maxAttempts: 0            # total attempts per call (1 = no retries); 0 = SDK default (3)
    mode: ""                  # standard (default) | adaptive (adds client-side
                              # rate limiting; best for accounts that hit throttling)

  allRegions: false           # true = query all available regions
  regions:
    - us-east-1

services:
  ec2:           { enabled: true }
  s3:            { enabled: true }
  rds:           { enabled: true }
  iam:           { enabled: true }
  dynamodb:      { enabled: true }
  lambda:        { enabled: true }
  emr:           { enabled: true }
  ecs:           { enabled: true }
  eks:           { enabled: true }
  elbv2:         { enabled: true }
  secretsmanager: { enabled: true }
  sqs:           { enabled: true }
  sns:           { enabled: true }
  cloudwatch:    { enabled: true }
  cloudfront:    { enabled: true }
  route53:       { enabled: true }

filters:
  regions: []                 # restrict to these regions (empty = use aws.regions)
  tags: {}                    # key: value tag filters
  states: []                  # filter by resource state (e.g. running, stopped)

output:
  format: table               # table | json
  includeDetails: false       # include extended resource details

ui:
  theme: spotted-pardalote    # active theme name (see themes.md)
```

### Customizing displayed columns

The VPC Explorer ships sensible default columns for each resource type, but you
can override which fields appear as table `columns` and which appear in the
`detail` overlay under `display.vpc.<resource>`. Resource keys match the service
keys (`subnets`, `security_groups`, `route_tables`, `internet_gateways`,
`nat_gateways`, `endpoints`, `network_acls`, `peering`, `flow_logs`,
`ec2_instances`, `lambda`, `rds`, `load_balancers`).

```yaml
display:
  vpc:
    subnets:
      columns: [name, cidr, az, available_ips, public]   # table columns, left→right
      detail:  [subnet_id, vpc_id, state, map_public_ip] # fields in the detail overlay
    security_groups:
      columns: [sg_id, name, inbound, outbound, description]
```

Any resource type you omit keeps its built-in defaults.

### Resilient scanning (retries & partial results)

Collection is **best-effort**. When a service/region fails partway through —
a later page throttles, or a per-item describe call is denied — everything
collected before the failure is kept and shown, and the error is reported as
*partial* (`partial results kept` on the CLI, `"partial": true` in JSON
errors, and a note in the TUI errors overlay). Previously a single failed page
discarded the whole service/region.

For large accounts that hit AWS throttling (`RequestLimitExceeded`,
`ThrottlingException`), tune the SDK retry behaviour under `aws.retry`:

```yaml
aws:
  retry:
    maxAttempts: 8       # keep retrying longer than the default 3 attempts
    mode: adaptive       # client-side rate limiting that backs off automatically
```

`adaptive` mode wraps the standard exponential backoff with a client-side
token bucket that slows the request rate after throttle responses — usually
the right choice for `--all-regions` sweeps of busy accounts. Leave the block
unset to keep the AWS SDK defaults (3 attempts, standard mode).
