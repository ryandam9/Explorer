package xref

import (
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		in       string
		wantKind Kind
		wantID   string
	}{
		{"arn:aws:iam::123456789012:role/app-task", KindIAMRole, "app-task"},
		{"arn:aws:iam::123456789012:role/path/to/app", KindIAMRole, "app"},
		{"arn:aws:kms:us-east-1:123456789012:key/abcd-1234", KindKMSKey, "abcd-1234"},
		{"arn:aws:acm:us-east-1:123456789012:certificate/xyz-9", KindACMCert, "xyz-9"},
		{"arn:aws:ec2:us-east-1:123456789012:security-group/sg-0abc", KindSecurityGroup, "sg-0abc"},
		{"sg-0abc123", KindSecurityGroup, "sg-0abc123"},
		{"my-role-name", KindIAMRole, "my-role-name"},
		{"arn:aws:s3:::some-bucket", KindUnknown, ""},
		// EC2-style resource ids must not be mislabelled as IAM role names.
		{"vpc-0475013d0d9249369", KindUnknown, "vpc-0475013d0d9249369"},
		{"subnet-0abc12345678", KindUnknown, "subnet-0abc12345678"},
		{"i-0abc12345678", KindUnknown, "i-0abc12345678"},
		{"eni-0abc12345678", KindUnknown, "eni-0abc12345678"},
		// Hyphenated role names whose tail isn't a hex token stay IAM roles.
		{"app-2", KindIAMRole, "app-2"},
		{"", KindUnknown, ""},
	}
	for _, c := range cases {
		got := Classify(c.in)
		if got.Kind != c.wantKind {
			t.Errorf("Classify(%q).Kind = %q, want %q", c.in, got.Kind, c.wantKind)
		}
		if got.ID != c.wantID {
			t.Errorf("Classify(%q).ID = %q, want %q", c.in, got.ID, c.wantID)
		}
	}
}

func TestWhereUsed_ResolvesArnAndIdInterchangeably(t *testing.T) {
	keyARN := "arn:aws:kms:us-east-1:123456789012:key/abcd-1234"
	edges := []Edge{
		// Edge stored by ARN; query will be by ARN.
		{From: Reference{Service: "ec2", Type: "volume", Region: "us-east-1", ID: "vol-1", Via: "volume encryption key"}, Target: keyARN},
		// Edge stored by bare key id; query by ARN must still find it.
		{From: Reference{Service: "secretsmanager", Type: "secret", Region: "us-east-1", ID: "arn:…:secret/db", Name: "db", Via: "secret encryption key"}, Target: "abcd-1234"},
		// Unrelated edge must not match.
		{From: Reference{Service: "lambda", Type: "function", Region: "us-east-1", ID: "fn", Via: "environment encryption key"}, Target: "other-key"},
	}
	idx := BuildIndex(edges)

	res := WhereUsed(Classify(keyARN), idx)
	if len(res.References) != 2 {
		t.Fatalf("want 2 references, got %d: %+v", len(res.References), res.References)
	}
	// Querying by the bare ID resolves the same set.
	resByID := WhereUsed(Classify("abcd-1234"), idx) // bare id classifies as iam-role, so query explicitly:
	_ = resByID
	target := Target{Kind: KindKMSKey, ID: "abcd-1234", Identifiers: []string{"abcd-1234"}}
	res2 := WhereUsed(target, idx)
	if len(res2.References) != 2 {
		t.Errorf("query by id: want 2 references, got %d", len(res2.References))
	}
}

func TestWhereUsed_NotReferencedIsScoped(t *testing.T) {
	idx := BuildIndex(nil)
	res := WhereUsed(Classify("arn:aws:iam::123456789012:role/lonely"), idx)
	if len(res.References) != 0 {
		t.Errorf("want 0 references, got %d", len(res.References))
	}
	if len(res.CheckedTypes) == 0 {
		t.Errorf("CheckedTypes must be populated so 'not referenced' is scoped")
	}
	// IAM-role checked types should mention Lambda execution roles.
	joined := strings.Join(res.CheckedTypes, ", ")
	if !strings.Contains(joined, "Lambda") {
		t.Errorf("IAM-role CheckedTypes missing Lambda: %q", joined)
	}
}

func TestWhereUsed_Deduplicates(t *testing.T) {
	roleARN := "arn:aws:iam::123456789012:role/app"
	// Same referencing resource keyed under both ARN and short form must appear once.
	edges := []Edge{
		{From: Reference{Service: "lambda", Type: "function", Region: "us-east-1", ID: "fn", Via: "execution role"}, Target: roleARN},
	}
	idx := BuildIndex(edges)
	res := WhereUsed(Classify(roleARN), idx)
	if len(res.References) != 1 {
		t.Errorf("want 1 deduped reference, got %d", len(res.References))
	}
	if res.References[0].Via != "execution role" {
		t.Errorf("Via = %q, want execution role", res.References[0].Via)
	}
}

func TestTrustPrincipals(t *testing.T) {
	// URL-encoded trust policy with a single AWS principal that is a role ARN.
	doc := `%7B%22Version%22%3A%222012-10-17%22%2C%22Statement%22%3A%5B%7B%22Effect%22%3A%22Allow%22%2C%22Principal%22%3A%7B%22AWS%22%3A%22arn%3Aaws%3Aiam%3A%3A123456789012%3Arole%2Fsource%22%7D%2C%22Action%22%3A%22sts%3AAssumeRole%22%7D%5D%7D`
	got := trustPrincipals(doc)
	if len(got) != 1 || got[0] != "arn:aws:iam::123456789012:role/source" {
		t.Errorf("trustPrincipals = %v, want [arn:…:role/source]", got)
	}

	// Service principals and "*" are ignored.
	plain := `{"Statement":[{"Principal":{"Service":"ec2.amazonaws.com"}},{"Principal":{"AWS":["arn:aws:iam::1:role/a","*"]}}]}`
	got = trustPrincipals(plain)
	if len(got) != 1 || got[0] != "arn:aws:iam::1:role/a" {
		t.Errorf("trustPrincipals = %v, want [arn:…:role/a]", got)
	}
}

func TestRender_TableAndJSON(t *testing.T) {
	roleARN := "arn:aws:iam::123456789012:role/app"
	idx := BuildIndex([]Edge{
		{From: Reference{Service: "lambda", Type: "function", Region: "us-east-1", ID: "arn:…:fn", Name: "checkout", Via: "execution role"}, Target: roleARN},
	})
	res := WhereUsed(Classify(roleARN), idx)

	var sb strings.Builder
	if err := Render(&sb, res, "table", false); err != nil {
		t.Fatalf("table: %v", err)
	}
	out := sb.String()
	for _, want := range []string{"checkout", "execution role", "Reference types checked"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}

	sb.Reset()
	empty := WhereUsed(Classify("arn:aws:iam::1:role/lonely"), BuildIndex(nil))
	if err := Render(&sb, empty, "table", false); err != nil {
		t.Fatalf("table empty: %v", err)
	}
	if !strings.Contains(sb.String(), "Not referenced by anything this tool checked") {
		t.Errorf("empty table missing scoped message:\n%s", sb.String())
	}

	sb.Reset()
	if err := Render(&sb, res, "json", false); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !strings.Contains(sb.String(), `"checked_types"`) || !strings.Contains(sb.String(), `"via": "execution role"`) {
		t.Errorf("json missing fields:\n%s", sb.String())
	}
}
