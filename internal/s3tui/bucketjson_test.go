package s3tui

import (
	"strings"
	"testing"
)

func TestPrettyJSON(t *testing.T) {
	out := prettyJSON(`{"a":1,"b":[2,3]}`)
	if !strings.Contains(out, "\n  \"a\": 1") {
		t.Errorf("compact JSON not indented:\n%s", out)
	}
	// Non-JSON is returned unchanged so the viewer still shows something.
	if got := prettyJSON("not json at all"); got != "not json at all" {
		t.Errorf("non-JSON should pass through, got %q", got)
	}
}

func TestOpenBucketPolicyJSON(t *testing.T) {
	m := &Model{width: 100, height: 30, detailBucket: "my-bucket"}

	// A real policy is shown, pretty-printed, with the bucket in the title.
	m.selectedBucketDetails = &BucketDetails{
		Policy:    "Set (Available)",
		RawPolicy: `{"Version":"2012-10-17","Statement":[]}`,
	}
	m.openBucketPolicyJSON()
	if !m.showBucketJSON {
		t.Fatal("viewer should be open")
	}
	if !strings.Contains(m.bucketJSONTitle, "my-bucket") {
		t.Errorf("title missing bucket name: %q", m.bucketJSONTitle)
	}
	if !strings.Contains(m.bucketJSONContent, "\"Version\": \"2012-10-17\"") {
		t.Errorf("policy not pretty-printed:\n%s", m.bucketJSONContent)
	}

	// No policy → an explanatory message, not a blank pane.
	m.selectedBucketDetails = &BucketDetails{Policy: "None"}
	m.openBucketPolicyJSON()
	if !strings.Contains(m.bucketJSONContent, "No bucket policy is set") {
		t.Errorf("expected 'no policy' message, got %q", m.bucketJSONContent)
	}

	// Access denied is surfaced as such (tri-state: unknown, not "none").
	m.selectedBucketDetails = &BucketDetails{Policy: "Access Denied"}
	m.openBucketPolicyJSON()
	if !strings.Contains(m.bucketJSONContent, "Access denied") {
		t.Errorf("expected access-denied message, got %q", m.bucketJSONContent)
	}
}

func TestOpenBucketCORSJSON(t *testing.T) {
	m := &Model{width: 100, height: 30, detailBucket: "cors-bucket"}
	m.selectedBucketDetails = &BucketDetails{
		CORS:     "1 rule(s)",
		CORSJSON: `[{"AllowedMethods":["GET"],"AllowedOrigins":["*"]}]`,
	}
	m.openBucketCORSJSON()
	if !m.showBucketJSON {
		t.Fatal("viewer should be open")
	}
	if !strings.Contains(m.bucketJSONContent, "AllowedMethods") {
		t.Errorf("CORS config not shown:\n%s", m.bucketJSONContent)
	}

	m.selectedBucketDetails = &BucketDetails{CORS: "Not configured"}
	m.openBucketCORSJSON()
	if !strings.Contains(m.bucketJSONContent, "No CORS configuration is set") {
		t.Errorf("expected 'no CORS' message, got %q", m.bucketJSONContent)
	}
}

// A nil details pointer must not open the viewer or panic.
func TestOpenBucketJSONNilDetails(t *testing.T) {
	m := &Model{width: 100, height: 30, detailBucket: "b"}
	m.openBucketPolicyJSON()
	m.openBucketCORSJSON()
	if m.showBucketJSON {
		t.Error("viewer should stay closed when there are no details")
	}
}
