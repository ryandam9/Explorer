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
| **IAM role** (`arn:…:role/app` or `app`) | Lambda execution roles, EC2 instance profiles, ECS task & execution roles, EKS cluster & node-group roles, IAM role trust policies |
| **KMS key** (`arn:…:key/<uuid>`) | EBS volume / RDS instance / Secrets Manager / SQS queue / Lambda environment encryption |
| **ACM certificate** (`arn:…:certificate/<id>`) | ELBv2 (ALB/NLB) listeners |
| **Security group** (`sg-…` or its ARN) | Elastic network interface attachments (account-wide) |

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
`lambda:ListFunctions`, `ec2:{DescribeInstances,DescribeVolumes,DescribeNetworkInterfaces}`,
`rds:DescribeDBInstances`, `secretsmanager:ListSecrets`,
`sqs:{ListQueues,GetQueueAttributes}`, `ecs:{ListTaskDefinitions,DescribeTaskDefinition}`,
`eks:{ListClusters,DescribeCluster,ListNodegroups,DescribeNodegroup}`,
`elasticloadbalancing:{DescribeLoadBalancers,DescribeListeners}`. Any denial
skips that source with a note.
