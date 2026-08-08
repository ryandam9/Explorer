package sqstui

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

func TestClipBodyCellMarksTruncation(t *testing.T) {
	short := clipBodyCell("hello world")
	if short != "hello world" {
		t.Errorf("short body should pass through, got %q", short)
	}

	long := clipBodyCell(strings.Repeat("a", 500))
	if len([]rune(long)) != maxBodyCell {
		t.Errorf("clipped cell should be %d runes, got %d", maxBodyCell, len([]rune(long)))
	}
	if !strings.HasSuffix(long, "…") {
		t.Error("a clipped cell must end with an ellipsis so it never reads as the whole body")
	}

	flat := clipBodyCell("line1\nline2\ttabbed")
	if strings.ContainsAny(flat, "\n\t") {
		t.Errorf("cell should be flattened, got %q", flat)
	}
}

func TestMsgTimeAndCountUnknownWhenAbsent(t *testing.T) {
	m := types.Message{Body: aws.String("x")}
	if got := msgSentTime(m); got != unknownValue {
		t.Errorf("missing SentTimestamp should render unknown, got %q", got)
	}
	if got := msgReceiveCount(m); got != unknownValue {
		t.Errorf("missing ApproximateReceiveCount should render unknown, got %q", got)
	}

	m.Attributes = map[string]string{"SentTimestamp": "1700000000000", "ApproximateReceiveCount": "3"}
	if got := msgSentTime(m); !strings.HasPrefix(got, "2023-11-14") {
		t.Errorf("SentTimestamp should render as a date, got %q", got)
	}
	if got := msgReceiveCount(m); got != "3" {
		t.Errorf("receive count = %q, want 3", got)
	}
}

func TestMessageRecordText(t *testing.T) {
	m := types.Message{
		MessageId: aws.String("id-1"),
		Body:      aws.String(`{"a":1,"b":{"c":2}}`),
		Attributes: map[string]string{
			"SentTimestamp":           "1700000000000",
			"ApproximateReceiveCount": "2",
			"MessageGroupId":          "group-1",
		},
		MessageAttributes: map[string]types.MessageAttributeValue{
			"traceId": {DataType: aws.String("String"), StringValue: aws.String("t-123")},
			"payload": {DataType: aws.String("Binary"), BinaryValue: []byte{1, 2, 3}},
		},
	}

	out := messageRecordText(m)
	for _, want := range []string{"id-1", "group-1", "traceId", "t-123", "<binary, 3 bytes>", `"a": 1`} {
		if !strings.Contains(out, want) {
			t.Errorf("record missing %q in:\n%s", want, out)
		}
	}
}

func TestPrettyBodyHandlesBOMAndNonJSON(t *testing.T) {
	if got := prettyBody("\uFEFF{\"a\":1}"); !strings.Contains(got, "\"a\": 1") {
		t.Errorf("BOM-prefixed JSON should pretty-print, got %q", got)
	}
	if got := prettyBody("plain text"); got != "plain text" {
		t.Errorf("non-JSON body should pass through, got %q", got)
	}
	if got := prettyBody("{broken"); got != "{broken" {
		t.Errorf("invalid JSON should pass through unchanged, got %q", got)
	}
}

func TestFormatMessages(t *testing.T) {
	out := formatMessages([]types.Message{
		testMessage("m1", "hello", map[string]string{"SentTimestamp": "1700000000000", "ApproximateReceiveCount": "1"}),
	})
	if !strings.Contains(out, "hello") || !strings.Contains(out, "recv=1") {
		t.Errorf("formatted output should carry body and receive count, got %q", out)
	}
}
