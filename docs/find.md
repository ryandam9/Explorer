# Find Usage

`find` answers "what is this thing?" for a mystery identifier — an ENI from
an error message, half a resource name from a ticket. It scans the configured
regions (typed collectors **plus** the all-services Tagging API sweep, so the
long tail of services is covered) and fuzzy-matches every resource against
the fragment; best matches print first.

The match is an in-order subsequence, so separators can be skipped:
`eni0abc` finds `eni-0abc12`, `prodweb` finds `prod-web-3`. Exact substrings,
word-start hits and shorter names rank higher.

```bash
# What is this ENI?
aws_explorer find eni-0abc

# Find by name fragment across every region
aws_explorer find prodweb --all-regions

# Machine-readable
aws_explorer find payments -o json | jq '.[0].arn'
```

```
SNO  NAME        TYPE           ID                     REGION      ARN
1    prod-web-3  ec2/instance   i-0abc12def34567890    us-east-1   arn:aws:ec2:us-east-1:…
2    prod-web    elbv2/loadb…   arn:aws:elasticloadb…  us-east-1   arn:aws:elasticloadb…
```

| Flag | Default | Description |
|------|---------|-------------|
| `--limit` | `25` | Maximum number of matches to print |
| `--output` / `-o` | `table` | `table`, `json`, `ndjson`, `csv` — always in best-match order |

The same search lives in the summary TUI behind **`Ctrl+P`**: a palette
fuzzy-matches as you type, `↑`/`↓` select, and `Enter` jumps straight to the
resource — its service selected in the sidebar, its row under the cursor, and
the detail panel open. Any active filters that would hide the target are
cleared.


# Whereused (blast radius)

`whereused` answers "can I delete this?" for the resources people actually ask
about — IAM roles, KMS keys, ACM certificates and security groups. It scans
the account for the linking fields the inventory does not keep (a Lambda's
execution role, a volume's KMS key, a listener's certificate, an ENI's
security groups) and lists every resource that references the target. It is
the CLI generalization of the VPC explorer's `x` cross-reference.

```bash
./bin/aws_explorer whereused <arn-or-id> [--all-regions] [-o table|json|ndjson|csv]
```

```
Where-used: app-task (iam-role)

SNO  SERVICE  TYPE             RESOURCE           REGION      VIA
1    ecs      task-definition  checkout:7         us-east-1   ECS task role
2    lambda   function         payments           us-east-1   execution role

Reference types checked: Lambda execution roles, EC2 instance profiles, ECS task and execution roles, EKS cluster and node-group roles, IAM role trust policies.
(Absence above means none of these reference it — not that nothing anywhere does.)
```

Accepted targets (full ARN or bare ID):

| Target | Reference types checked |
|--------|-------------------------|
| **IAM role** (`arn:…:role/app` or `app`) | Lambda execution roles, EC2 instance profiles, ECS task & execution roles, EKS cluster & node-group roles, IAM role trust policies, S3 bucket replication roles |
| **KMS key** (`arn:…:key/<uuid>`) | EBS volume / RDS instance / Secrets Manager / SQS queue / Lambda environment / S3 bucket default / EFS file system encryption |
| **ACM certificate** (`arn:…:certificate/<id>`) | ELBv2 (ALB/NLB) listeners |
| **Security group** (`sg-…` or its ARN) | Elastic network interface attachments, EFS mount target security groups, Lambda VPC security groups, EKS cluster security groups (account-wide) |

> **Scoped "not referenced".** The report always prints the reference types it
> checked. Absence of evidence is therefore explicitly bounded — it means none
> of *those* reference types point at the target, not that nothing anywhere
> does. A denied or failed API call narrows what was checked (reported on
> stderr) and never aborts the run. KMS keys referenced by alias are matched on
> the raw alias string rather than resolved to the key.

| Flag | Default | Description |
|------|---------|-------------|
| `--output` / `-o` | `table` | `table`, `json`, `ndjson`, `csv` |

**IAM permissions.** Read-only: `iam:{ListRoles,ListInstanceProfiles}`,
`lambda:{ListFunctions,ListEventSourceMappings}`,
`ec2:{DescribeInstances,DescribeVolumes,DescribeNetworkInterfaces,DescribeAddresses}`,
`rds:DescribeDBInstances`, `secretsmanager:ListSecrets`,
`sqs:{ListQueues,GetQueueAttributes}`, `ecs:{ListTaskDefinitions,DescribeTaskDefinition}`,
`eks:{ListClusters,DescribeCluster,ListNodegroups,DescribeNodegroup}`,
`elasticloadbalancing:{DescribeLoadBalancers,DescribeListeners}`,
`s3:{ListAllMyBuckets,GetBucketNotification,GetReplicationConfiguration,GetBucketLogging,GetEncryptionConfiguration}`,
`elasticfilesystem:{DescribeFileSystems,DescribeMountTargets,DescribeMountTargetSecurityGroups}`.
Any denial skips that source with a note.

# Related (bidirectional)

`related` generalizes `whereused` to **both directions** for **any** resource:
what a resource *uses* (forward — its execution role, KMS key, security groups,
…) **and** what *uses* it (reverse — the `whereused` answer). It reuses the same
account scan, so it sees the same relationship types `whereused` does (today:
Lambda/EC2/ECS/EKS roles, EBS/RDS/Secrets/SQS/Lambda/S3/EFS KMS keys, ENI /
EFS-mount-target / Lambda-VPC / EKS security groups, ELBv2 listener certs, IAM
trust principals, **S3 event notifications → Lambda/SNS/SQS**, S3 replication &
access-logging, **Lambda event-source mappings** (SQS/DynamoDB/Kinesis/MSK),
Lambda layers / dead-letter / VPC / log group, EC2 subnet/AMI/key-pair/ENI/EIP,
EBS attachments, ECS container log groups & secrets, EFS mount-target subnets,
EKS subnets & OIDC provider); coverage grows under the
[related-resources epic](https://github.com/ryandam9/aws_explorer/issues/336).

```bash
./bin/aws_explorer related <arn-or-id> [--depth N] [--direction both|uses|usedby] [--all-regions] [-o table|json|ndjson|csv]
```

```
Related: app (iam-role)

Uses (depends on) →
SNO  SERVICE  TYPE  RESOURCE  REGION  VIA
1    iam      role  source    -       trust policy principal

Only relationships this tool extracts are shown; un-collected link types won't appear.

Used by ←
SNO  SERVICE  TYPE             RESOURCE     REGION      VIA
1    ecs      task-definition  web          us-east-1   ECS task role
2    lambda   function         checkout     us-east-1   execution role

Reference types checked: Lambda execution roles, EC2 instance profiles, ECS task and execution roles, EKS cluster and node-group roles, IAM role trust policies.

Only relationships this tool extracts are shown; un-collected link types won't appear.
```

| Flag | Default | Description |
|------|---------|-------------|
| `--depth` | `1` | how many hops to follow (1–5); e.g. `--depth 2` walks a Lambda → its role → that role's trust principals |
| `--direction` | `both` | `both`, `uses` (forward only), or `usedby` (reverse only) |
| `--output` / `-o` | `table` | `table`, `json`, `ndjson`, `csv` (CSV cells sanitized against formula injection) |

> **Scoped, honest results.** Like `whereused`, an empty side means "none of the
> relationship types this tool collects" — never "this resource is isolated".
> The caveat is printed on every run, and the reverse direction lists the
> reference types checked for recognized kinds. Best-effort: a denied/failed API
> call narrows what was checked (reported on stderr) and never aborts the run.

**IAM permissions.** Same as `whereused` above (the two share one account scan).
