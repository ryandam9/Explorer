package trail

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// Fixture CloudTrail event records, per the shapes documented in the
// CloudTrail userIdentity element reference.

const assumedRoleEvent = `{
  "eventVersion": "1.09",
  "eventName": "AuthorizeSecurityGroupIngress",
  "readOnly": false,
  "sourceIPAddress": "203.0.113.7",
  "userIdentity": {
    "type": "AssumedRole",
    "principalId": "AROAEXAMPLE:deploy-session",
    "arn": "arn:aws:sts::123456789012:assumed-role/deploy-pipeline/deploy-session",
    "accountId": "123456789012",
    "sessionContext": {
      "sessionIssuer": {
        "type": "Role",
        "arn": "arn:aws:iam::123456789012:role/deploy-pipeline"
      }
    }
  }
}`

const iamUserEvent = `{
  "eventName": "ModifySecurityGroupRules",
  "readOnly": false,
  "sourceIPAddress": "198.51.100.2",
  "userIdentity": {
    "type": "IAMUser",
    "principalId": "AIDAEXAMPLE",
    "arn": "arn:aws:iam::123456789012:user/alice",
    "accountId": "123456789012",
    "userName": "alice"
  }
}`

const rootEvent = `{
  "eventName": "CreateVpc",
  "readOnly": false,
  "sourceIPAddress": "192.0.2.1",
  "userIdentity": {
    "type": "Root",
    "principalId": "123456789012",
    "arn": "arn:aws:iam::123456789012:root",
    "accountId": "123456789012"
  }
}`

const serviceEvent = `{
  "eventName": "CreateNetworkInterface",
  "readOnly": false,
  "sourceIPAddress": "lambda.amazonaws.com",
  "userIdentity": {
    "type": "AWSService",
    "invokedBy": "lambda.amazonaws.com"
  }
}`

const readOnlyEvent = `{
  "eventName": "DescribeSecurityGroups",
  "readOnly": true,
  "sourceIPAddress": "198.51.100.2",
  "userIdentity": {
    "type": "IAMUser",
    "arn": "arn:aws:iam::123456789012:user/alice"
  }
}`

const deniedEvent = `{
  "eventName": "RunInstances",
  "readOnly": false,
  "sourceIPAddress": "198.51.100.2",
  "errorCode": "Client.UnauthorizedOperation",
  "userIdentity": {
    "type": "IAMUser",
    "arn": "arn:aws:iam::123456789012:user/alice"
  }
}`

func TestSummarize_ExtractsErrorCode(t *testing.T) {
	ev := summarize("alice", "false", deniedEvent)
	if ev.ErrorCode != "Client.UnauthorizedOperation" {
		t.Errorf("ErrorCode = %q, want Client.UnauthorizedOperation", ev.ErrorCode)
	}
	// A successful call carries no errorCode.
	if ev := summarize("alice", "false", iamUserEvent); ev.ErrorCode != "" {
		t.Errorf("ErrorCode = %q, want empty for a successful call", ev.ErrorCode)
	}
}

func TestFilterAttribute(t *testing.T) {
	cases := []struct {
		name     string
		filter   Filter
		wantKey  string
		wantVal  string
		wantNone bool
	}{
		{"empty is account-wide", Filter{}, "", "", true},
		{"resource", Filter{ResourceName: "i-0abc"}, "ResourceName", "i-0abc", false},
		{"principal", Filter{Principal: "alice"}, "Username", "alice", false},
		{"event name", Filter{EventName: "RunInstances"}, "EventName", "RunInstances", false},
		{"event source", Filter{EventSource: "ec2.amazonaws.com"}, "EventSource", "ec2.amazonaws.com", false},
		// ResourceName wins when several are set (callers should set only one).
		{"resource precedence", Filter{ResourceName: "i-0abc", Principal: "alice"}, "ResourceName", "i-0abc", false},
	}
	for _, c := range cases {
		attr, ok := c.filter.attribute()
		if c.wantNone {
			if ok {
				t.Errorf("%s: expected no attribute, got %v", c.name, attr)
			}
			continue
		}
		if !ok {
			t.Errorf("%s: expected an attribute, got none", c.name)
			continue
		}
		if string(attr.AttributeKey) != c.wantKey || *attr.AttributeValue != c.wantVal {
			t.Errorf("%s: attribute = %s/%s, want %s/%s",
				c.name, attr.AttributeKey, *attr.AttributeValue, c.wantKey, c.wantVal)
		}
	}
}

func TestSummarize_AssumedRole(t *testing.T) {
	ev := summarize("deploy-session", "false", assumedRoleEvent)
	if ev.Principal != "role/deploy-pipeline" {
		t.Errorf("Principal = %q, want role/deploy-pipeline", ev.Principal)
	}
	if ev.SourceIP != "203.0.113.7" {
		t.Errorf("SourceIP = %q", ev.SourceIP)
	}
	if ev.ReadOnly {
		t.Error("ReadOnly = true, want false")
	}
}

func TestSummarize_IAMUser(t *testing.T) {
	ev := summarize("alice", "false", iamUserEvent)
	if ev.Principal != "user/alice" {
		t.Errorf("Principal = %q, want user/alice", ev.Principal)
	}
	if ev.SourceIP != "198.51.100.2" {
		t.Errorf("SourceIP = %q", ev.SourceIP)
	}
}

func TestSummarize_Root(t *testing.T) {
	ev := summarize("root", "false", rootEvent)
	if ev.Principal != "root (123456789012)" {
		t.Errorf("Principal = %q, want root (123456789012)", ev.Principal)
	}
}

func TestSummarize_AWSService(t *testing.T) {
	ev := summarize("", "false", serviceEvent)
	if ev.Principal != "lambda.amazonaws.com" {
		t.Errorf("Principal = %q, want lambda.amazonaws.com", ev.Principal)
	}
}

func TestSummarize_ReadOnly(t *testing.T) {
	// readOnly comes from the LookupEvents response field when present…
	if ev := summarize("alice", "true", readOnlyEvent); !ev.ReadOnly {
		t.Error("ReadOnly = false, want true (from response field)")
	}
	// …and from the record JSON when the response field is empty.
	if ev := summarize("alice", "", readOnlyEvent); !ev.ReadOnly {
		t.Error("ReadOnly = false, want true (from record JSON)")
	}
}

func TestSummarize_UnparsableJSONFallsBackToUsername(t *testing.T) {
	ev := summarize("alice", "false", "not json")
	if ev.Principal != "alice" {
		t.Errorf("Principal = %q, want alice", ev.Principal)
	}
	if ev.SourceIP != "-" {
		t.Errorf("SourceIP = %q, want -", ev.SourceIP)
	}
}

func TestSummarize_EmptyEverything(t *testing.T) {
	ev := summarize("", "", "")
	if ev.Principal != "-" {
		t.Errorf("Principal = %q, want -", ev.Principal)
	}
}

func TestShortPrincipal(t *testing.T) {
	cases := []struct{ in, want string }{
		{"arn:aws:sts::123456789012:assumed-role/deploy-pipeline/session", "role/deploy-pipeline"},
		{"arn:aws:iam::123456789012:role/app", "role/app"},
		{"arn:aws:iam::123456789012:role/service-role/my-lambda-role", "role/service-role/my-lambda-role"},
		{"arn:aws:iam::123456789012:user/alice", "user/alice"},
		{"arn:aws:iam::123456789012:root", "root"},
		{"cloudformation.amazonaws.com", "cloudformation.amazonaws.com"},
	}
	for _, c := range cases {
		if got := ShortPrincipal(c.in); got != c.want {
			t.Errorf("ShortPrincipal(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLookupValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{"i-0abc12345", "i-0abc12345"},
		{"arn:aws:ec2:us-east-1:123456789012:instance/i-0abc12345", "i-0abc12345"},
		{"arn:aws:lambda:us-east-1:123456789012:function:my-fn", "my-fn"},
		{"arn:aws:s3:::my-bucket", "my-bucket"},
		{"arn:aws:iam::123456789012:role/my-role", "my-role"},
		{"arn:aws:sqs:us-east-1:123456789012:my-queue", "my-queue"},
		{"  sg-0abc  ", "sg-0abc"},
		{"arn:incomplete", "arn:incomplete"},
	}
	for _, c := range cases {
		if got := LookupValue(c.in); got != c.want {
			t.Errorf("LookupValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRender_Table(t *testing.T) {
	events := []Event{
		{
			Time:      time.Date(2026, 6, 11, 14, 2, 0, 0, time.UTC),
			EventName: "AuthorizeSecurityGroupIngress",
			Principal: "role/deploy-pipeline",
			SourceIP:  "203.0.113.7",
		},
		{
			Time:      time.Date(2026, 6, 9, 9, 15, 0, 0, time.UTC),
			EventName: "DescribeSecurityGroups",
			Principal: "user/alice",
			SourceIP:  "198.51.100.2",
			ReadOnly:  true,
		},
	}
	var buf bytes.Buffer
	if err := Render(&buf, events, "table", false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"SNO", "TIME", "OUTCOME", "AuthorizeSecurityGroupIngress", "role/deploy-pipeline",
		"203.0.113.7", "DescribeSecurityGroups (read)", "ok",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
}

func TestRender_TableShowsErrorCode(t *testing.T) {
	events := []Event{{
		Time:      time.Date(2026, 6, 11, 14, 2, 0, 0, time.UTC),
		EventName: "RunInstances",
		Principal: "user/alice",
		SourceIP:  "198.51.100.2",
		ErrorCode: "AccessDenied",
	}}
	var buf bytes.Buffer
	if err := Render(&buf, events, "table", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "AccessDenied") {
		t.Errorf("table should show the errorCode in OUTCOME:\n%s", buf.String())
	}
}

func TestRender_JSONEmptyIsArray(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, nil, "json", false); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("empty json = %q, want []", buf.String())
	}
}

func TestRender_CSV(t *testing.T) {
	events := []Event{{
		Time:      time.Date(2026, 6, 11, 14, 2, 0, 0, time.UTC),
		EventName: "CreateVpc",
		Principal: "root",
		SourceIP:  "192.0.2.1",
	}}
	var buf bytes.Buffer
	if err := Render(&buf, events, "csv", false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("csv lines = %d, want 2:\n%s", len(lines), buf.String())
	}
	if lines[0] != "Time,Event,Principal,SourceIP,ReadOnly,ErrorCode" {
		t.Errorf("csv header = %q", lines[0])
	}
	if !strings.Contains(lines[1], "2026-06-11T14:02:05Z"[:11]) {
		t.Errorf("csv row = %q", lines[1])
	}
}
