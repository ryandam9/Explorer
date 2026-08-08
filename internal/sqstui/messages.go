package sqstui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/mattn/go-runewidth"

	"github.com/ryandam9/aws_explorer/internal/table"
)

// maxBodyCell caps how many runes of a message body go into a table cell; the
// shared table sizes columns to their widest cell, so an unclipped megabyte
// body would blow up layout. The full body is always available via the record
// view (v) and copy (y) — a clipped cell is marked with an ellipsis so it is
// never mistaken for the whole body.
const maxBodyCell = 160

// flattenBody puts a message body on one line for a table cell.
func flattenBody(s string) string {
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
}

// clipBodyCell flattens and truncates a body for its table cell.
func clipBodyCell(s string) string {
	r := []rune(flattenBody(s))
	if len(r) <= maxBodyCell {
		return string(r)
	}
	return string(r[:maxBodyCell-1]) + "…"
}

// msgSentTime renders a message's SentTimestamp system attribute (epoch ms).
// A missing attribute renders as unknown, never as the epoch zero time.
func msgSentTime(m types.Message) string {
	v := m.Attributes["SentTimestamp"]
	if v == "" {
		return unknownValue
	}
	var ms int64
	if _, err := fmt.Sscanf(v, "%d", &ms); err != nil {
		return unknownValue
	}
	return time.Unix(0, ms*int64(time.Millisecond)).Format("2006-01-02 15:04:05")
}

// msgReceiveCount renders a message's ApproximateReceiveCount, unknown when
// absent. This is the count AFTER the peek that fetched it.
func msgReceiveCount(m types.Message) string {
	if v := m.Attributes["ApproximateReceiveCount"]; v != "" {
		return v
	}
	return unknownValue
}

// messageTableColumns is the peek table layout: sent time and receive count
// pinned, body last.
func messageTableColumns() []table.Column {
	return []table.Column{
		{Title: "Sent", Width: 4},
		{Title: "Recv", Width: 4},
		{Title: "Body", Width: 4},
	}
}

// messageTableRows maps sampled messages onto rows matching
// messageTableColumns.
func messageTableRows(msgs []types.Message) []table.Row {
	rows := make([]table.Row, 0, len(msgs))
	for _, m := range msgs {
		rows = append(rows, table.Row{
			msgSentTime(m),
			msgReceiveCount(m),
			clipBodyCell(aws.ToString(m.Body)),
		})
	}
	return rows
}

// messageRecordText renders one message vertically with every field's full
// value — the escape hatch from body-cell truncation. A body that parses as
// JSON is pretty-printed; anything else is kept verbatim.
func messageRecordText(m types.Message) string {
	var b strings.Builder
	add := func(label, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%-14s: %s\n", label, value)
		}
	}
	add("MessageId", aws.ToString(m.MessageId))
	add("Sent", msgSentTime(m))
	add("ReceiveCount", msgReceiveCount(m))
	add("SenderId", m.Attributes["SenderId"])
	add("GroupId", m.Attributes["MessageGroupId"])
	add("DedupId", m.Attributes["MessageDeduplicationId"])
	add("SequenceNo", m.Attributes["SequenceNumber"])

	// Custom message attributes, sorted for a stable rendering.
	if len(m.MessageAttributes) > 0 {
		keys := make([]string, 0, len(m.MessageAttributes))
		for k := range m.MessageAttributes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("\nMessage attributes:\n")
		for _, k := range keys {
			av := m.MessageAttributes[k]
			val := aws.ToString(av.StringValue)
			if val == "" && len(av.BinaryValue) > 0 {
				val = fmt.Sprintf("<binary, %d bytes>", len(av.BinaryValue))
			}
			fmt.Fprintf(&b, "  %s (%s): %s\n", k, aws.ToString(av.DataType), val)
		}
	}

	b.WriteString("\nBody:\n")
	b.WriteString(prettyBody(aws.ToString(m.Body)))
	return strings.TrimRight(b.String(), "\n")
}

// prettyBody indents a JSON body for the record view; non-JSON bodies are
// returned unchanged. A UTF-8 BOM is stripped first — TrimSpace does not
// remove it and it would defeat the JSON detection.
func prettyBody(body string) string {
	s := strings.TrimSpace(strings.TrimPrefix(body, "\uFEFF"))
	if len(s) == 0 || (s[0] != '{' && s[0] != '[') {
		return body
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, []byte(s), "", "  ") != nil {
		return body
	}
	return pretty.String()
}

// formatMessages renders sampled messages as timestamped plain-text lines, the
// shared shape for clipboard copies and file exports.
func formatMessages(msgs []types.Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(fmt.Sprintf("[%s] recv=%s %s\n", msgSentTime(m), msgReceiveCount(m), aws.ToString(m.Body)))
	}
	return sb.String()
}

// wrapLine hard-wraps a line to a display width, indenting wrapped
// continuations. Width is measured in terminal cells, not runes — wide
// characters (CJK, emoji) occupy two cells, and a rune-counted wrap would
// overflow the row and get clipped.
func wrapLine(line string, width int, indent string) []string {
	if runewidth.StringWidth(line) <= width {
		return []string{line}
	}
	contW := width - runewidth.StringWidth(indent)
	if contW < 10 {
		contW = 10
	}
	var out []string
	var seg strings.Builder
	segW := 0
	limit := width // the first segment keeps the full width
	flush := func() {
		if len(out) == 0 {
			out = append(out, seg.String())
		} else {
			out = append(out, indent+seg.String())
		}
		seg.Reset()
		segW = 0
		limit = contW
	}
	for _, r := range line {
		rw := runewidth.RuneWidth(r)
		if segW+rw > limit && segW > 0 {
			flush()
		}
		seg.WriteRune(r)
		segW += rw
	}
	if segW > 0 {
		flush()
	}
	return out
}

// sanitizeLine prepares a raw line for cell-accurate wrapping: tabs become
// four spaces (lipgloss would otherwise expand them after the wrap math) and
// stray carriage returns are dropped.
func sanitizeLine(s string) string {
	if !strings.ContainsAny(s, "\t\r") {
		return s
	}
	s = strings.ReplaceAll(s, "\t", "    ")
	return strings.ReplaceAll(s, "\r", "")
}
