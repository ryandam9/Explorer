package xref

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsiam "github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	awskms "github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
)

// Security/identity edge extractors (#343): IAM role → its policies (the second
// hop of `related --depth 2` on a Lambda's role), KMS key → policy principals,
// grants and aliases. Tri-state honesty (§8): a denied policy read is recorded,
// never silently rendered as "no policies".

// --- IAM role policies --------------------------------------------------------

// attachedPolicyEdges maps a role's attached managed policies to edges.
func attachedPolicyEdges(roleRef Reference, attached []iamtypes.AttachedPolicy) []Edge {
	var edges []Edge
	for _, p := range attached {
		if arn := aws.ToString(p.PolicyArn); arn != "" {
			edges = append(edges, Edge{From: withVia(roleRef, "attached managed policy"), Target: arn})
		}
	}
	return edges
}

// inlinePolicyEdges maps a role's inline policy names to edges. Inline policies
// are not standalone resources, so the target is a "<role>/<policy>" label.
func inlinePolicyEdges(roleRef Reference, names []string) []Edge {
	var edges []Edge
	for _, n := range names {
		if n == "" {
			continue
		}
		edges = append(edges, Edge{From: withVia(roleRef, "inline policy"), Target: roleRef.Name + "/" + n})
	}
	return edges
}

// rolePolicyEdges lists a role's attached managed and inline policies.
func rolePolicyEdges(ctx context.Context, client *awsiam.Client, roleRef Reference, rec *recorder) []Edge {
	name := roleRef.Name
	if name == "" {
		return nil
	}
	var edges []Edge

	ap := awsiam.NewListAttachedRolePoliciesPaginator(client, &awsiam.ListAttachedRolePoliciesInput{RoleName: &name})
	for ap.HasMorePages() {
		page, err := ap.NextPage(ctx)
		if err != nil {
			rec.record("iam", err)
			break
		}
		edges = append(edges, attachedPolicyEdges(roleRef, page.AttachedPolicies)...)
	}

	ip := awsiam.NewListRolePoliciesPaginator(client, &awsiam.ListRolePoliciesInput{RoleName: &name})
	for ip.HasMorePages() {
		page, err := ip.NextPage(ctx)
		if err != nil {
			rec.record("iam", err)
			break
		}
		edges = append(edges, inlinePolicyEdges(roleRef, page.PolicyNames)...)
	}
	return edges
}

// --- KMS ----------------------------------------------------------------------

// kmsKeyPolicyEdges maps the AWS principal ARNs named in a key policy to edges.
func kmsKeyPolicyEdges(keyRef Reference, principals []string) []Edge {
	var edges []Edge
	for _, p := range principals {
		edges = append(edges, Edge{From: withVia(keyRef, "key policy principal"), Target: p})
	}
	return edges
}

// kmsGrantEdges maps a key's grants to their grantee principals (ARNs only).
func kmsGrantEdges(keyRef Reference, grants []kmstypes.GrantListEntry) []Edge {
	var edges []Edge
	for _, g := range grants {
		if p := aws.ToString(g.GranteePrincipal); isARN(p) {
			edges = append(edges, Edge{From: withVia(keyRef, "grant grantee"), Target: p})
		}
	}
	return edges
}

// kmsAliasEdges maps each alias to the key it points at.
func kmsAliasEdges(aliases []kmstypes.AliasListEntry, region string) []Edge {
	var edges []Edge
	for _, a := range aliases {
		target := aws.ToString(a.TargetKeyId)
		if target == "" {
			continue
		}
		from := Reference{Service: "kms", Type: "alias", Region: region,
			ID: aws.ToString(a.AliasArn), Name: aws.ToString(a.AliasName)}
		if from.ID == "" {
			from.ID = aws.ToString(a.AliasName)
		}
		edges = append(edges, Edge{From: withVia(from, "alias target key"), Target: target})
	}
	return edges
}

func kmsEdges(ctx context.Context, cfg aws.Config, region string, rec *recorder) []Edge {
	client := awskms.NewFromConfig(cfg)
	var edges []Edge

	alp := awskms.NewListAliasesPaginator(client, &awskms.ListAliasesInput{})
	for alp.HasMorePages() {
		page, err := alp.NextPage(ctx)
		if err != nil {
			rec.record("kms", err)
			break
		}
		edges = append(edges, kmsAliasEdges(page.Aliases, region)...)
	}

	kp := awskms.NewListKeysPaginator(client, &awskms.ListKeysInput{})
	for kp.HasMorePages() {
		page, err := kp.NextPage(ctx)
		if err != nil {
			rec.record("kms", err)
			break
		}
		for _, k := range page.Keys {
			keyID := aws.ToString(k.KeyId)
			keyRef := Reference{Service: "kms", Type: "key", Region: region,
				ID: aws.ToString(k.KeyArn), Name: keyID}
			if pol, err := client.GetKeyPolicy(ctx, &awskms.GetKeyPolicyInput{KeyId: &keyID, PolicyName: aws.String("default")}); err != nil {
				rec.record("kms", err)
			} else {
				edges = append(edges, kmsKeyPolicyEdges(keyRef, trustPrincipals(aws.ToString(pol.Policy)))...)
			}
			gp := awskms.NewListGrantsPaginator(client, &awskms.ListGrantsInput{KeyId: &keyID})
			for gp.HasMorePages() {
				gPage, err := gp.NextPage(ctx)
				if err != nil {
					rec.record("kms", err)
					break
				}
				edges = append(edges, kmsGrantEdges(keyRef, gPage.Grants)...)
			}
		}
	}
	return edges
}
