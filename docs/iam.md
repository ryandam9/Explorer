# IAM Tools

Helpers for the most common AWS support question: *"why am I denied?"*

Two tools: `iam decode` explains a denial you already hit; `iam can` predicts
one before you hit it.

### Decode authorization failure messages

Services like EC2 redact *why* a request was denied into an opaque blob:

```
An error occurred (UnauthorizedOperation): You are not authorized to perform
this operation. Encoded authorization failure message: AQoDYXdzEJr…
```

`iam decode` calls `sts:DecodeAuthorizationMessage` and answers the three
questions that matter — who, what, on which resource — and whether it was an
**explicit deny** (a policy forbids it) or an **implicit deny** (no policy
allows it), which determines the fix:

```bash
# Pass the blob — or paste the entire error message; the blob is extracted
aws_explorer iam decode AQoDYXdzEJr…
pbpaste | aws_explorer iam decode

# Decoded JSON only, for jq
aws_explorer iam decode AQoDYXdzEJr… -o json
```

```
❌ Implicit deny — no policy allows this request (missing allow, not an explicit deny)
  Principal  arn:aws:iam::123456789012:user/bob
  Action     ec2:RunInstances
  Resource   arn:aws:ec2:us-east-1:123456789012:instance/*

  Fix: grant the principal an identity or resource policy that allows the action on the resource.

Full decoded document:
{ … }
```

Requires the `sts:DecodeAuthorizationMessage` IAM permission (a denial tells
you exactly that). The global `--profile`, `--auth-method`, `--role-arn` and
`--region` flags apply.

### Simulate policy — "can X do Y on Z?"

`iam can` runs `iam:SimulatePrincipalPolicy` and renders the verdict in the
path tracer's step-by-step style:

```bash
aws_explorer iam can arn:aws:iam::123456789012:role/app s3:GetObject arn:aws:s3:::my-bucket/key
```

```
❌ Denied: s3:GetObject on arn:aws:s3:::my-bucket/key for role/app — implicit deny (no policy allows it)
  ✗ Identity policies      no attached or inline policy allows this action
    Fix: grant an identity policy that allows s3:GetObject on arn:aws:s3:::my-bucket/key
  ✗ Permissions boundary   the boundary does not include this action — the boundary, not the identity policies, is the blocker

Caveats — the simulator does not evaluate:
  • Resource-based policies (bucket/queue/key/secret policies) — …
```

The three outcomes render distinctly: **allowed** (named the policy that
allows it), **implicit deny** (nothing allows it — add an allow), and
**explicit deny** (a policy forbids it — removing an allow elsewhere will
not help), with permissions-boundary and SCP effects called out when AWS
reports them. The action accepts a comma-separated list to check several at
once; `-o json` emits the verdicts for automation.

The caveats are printed with **every** verdict, because the simulator's
blind spots (resource-based policies, session policies, unsupplied condition
keys) are exactly what makes "but the simulator said allowed!" tickets.

Requires `iam:SimulatePrincipalPolicy`. Read-only — simulation never touches
the real resource.
