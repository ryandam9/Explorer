package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadDecodeInput(t *testing.T) {
	// From the argument.
	got, err := readDecodeInput([]string{"AQoDYXdz"}, strings.NewReader(""))
	if err != nil || got != "AQoDYXdz" {
		t.Errorf("arg input = %q, %v", got, err)
	}

	// From stdin via "-".
	got, err = readDecodeInput([]string{"-"}, strings.NewReader("Encoded authorization failure message: AQoDstdin\n"))
	if err != nil || got != "AQoDstdin" {
		t.Errorf("stdin input = %q, %v", got, err)
	}

	// From stdin with no args at all.
	got, err = readDecodeInput(nil, strings.NewReader("AQoDplain"))
	if err != nil || got != "AQoDplain" {
		t.Errorf("no-arg stdin input = %q, %v", got, err)
	}

	// Empty input errors.
	if _, err = readDecodeInput(nil, strings.NewReader("   ")); err == nil {
		t.Error("empty input should error")
	}
}

func TestRenderDecodedHuman(t *testing.T) {
	doc := `{"allowed":false,"explicitDeny":false,"context":{"principal":{"arn":"arn:aws:iam::1:user/bob"},"action":"ec2:RunInstances","resource":"*"}}`
	var buf bytes.Buffer
	if err := renderDecoded(&buf, []byte(doc), "table"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"Implicit deny", "user/bob", "Full decoded document:", `"allowed": false`} {
		if !strings.Contains(out, want) {
			t.Errorf("human output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderDecodedJSON(t *testing.T) {
	doc := `{"allowed":true}`
	var buf bytes.Buffer
	if err := renderDecoded(&buf, []byte(doc), "json"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "Full decoded document") || strings.Contains(out, "Allowed\n") {
		t.Errorf("-o json should emit only the document:\n%s", out)
	}
	if !strings.Contains(out, `"allowed": true`) {
		t.Errorf("json output = %q", out)
	}
}
