package sqstui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// unknownValue marks an attribute whose value could not be read — distinct
// from a real zero or empty value, so a denied or failed read never renders
// as fact (§ "not set" vs "denied" vs "failed").
const unknownValue = "—"

// Redrive is a queue's parsed RedrivePolicy: where over-received messages go
// and after how many receives.
type Redrive struct {
	TargetARN       string
	MaxReceiveCount int
}

// parseRedrive parses the RedrivePolicy attribute JSON. SQS documents
// maxReceiveCount as a number but has historically returned it as a JSON
// string, so both encodings are accepted. ok is false when the attribute is
// absent or unparseable.
func parseRedrive(raw string) (Redrive, bool) {
	if strings.TrimSpace(raw) == "" {
		return Redrive{}, false
	}
	var doc struct {
		TargetARN       string          `json:"deadLetterTargetArn"`
		MaxReceiveCount json.RawMessage `json:"maxReceiveCount"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil || doc.TargetARN == "" {
		return Redrive{}, false
	}
	r := Redrive{TargetARN: doc.TargetARN}
	count := strings.Trim(strings.TrimSpace(string(doc.MaxReceiveCount)), `"`)
	if n, err := strconv.Atoi(count); err == nil {
		r.MaxReceiveCount = n
	}
	return r, true
}

// queueNameFromARN extracts the queue name (the ARN's last :-segment).
func queueNameFromARN(arn string) string {
	if i := strings.LastIndexByte(arn, ':'); i >= 0 {
		return arn[i+1:]
	}
	return arn
}

// attrCount renders an approximate-count attribute. SQS counts are
// approximations by design, so real values carry a "~" prefix; a missing or
// unparseable value renders as unknown, never as 0.
func attrCount(attrs map[string]string, key string) string {
	v, ok := attrs[key]
	if !ok {
		return unknownValue
	}
	if _, err := strconv.ParseInt(v, 10, 64); err != nil {
		return unknownValue
	}
	return "~" + v
}

// attrSeconds renders a seconds-valued attribute as a compact duration
// ("30s", "5m", "4d"). Unknown values render as unknown, not as 0s.
func attrSeconds(attrs map[string]string, key string) string {
	v, ok := attrs[key]
	if !ok {
		return unknownValue
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return unknownValue
	}
	return formatSeconds(n)
}

// formatSeconds renders a second count compactly, preferring the largest
// whole unit (SQS attributes are whole seconds; retention is often days).
func formatSeconds(n int64) string {
	switch {
	case n >= 86400 && n%86400 == 0:
		return fmt.Sprintf("%dd", n/86400)
	case n >= 3600 && n%3600 == 0:
		return fmt.Sprintf("%dh", n/3600)
	case n >= 60 && n%60 == 0:
		return fmt.Sprintf("%dm", n/60)
	default:
		return fmt.Sprintf("%ds", n)
	}
}

// attrEpoch renders an epoch-seconds attribute as a local timestamp; unknown
// values render as unknown.
func attrEpoch(attrs map[string]string, key string) string {
	v, ok := attrs[key]
	if !ok {
		return unknownValue
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return unknownValue
	}
	return time.Unix(n, 0).Format("2006-01-02 15:04:05")
}

// attrBytes renders a byte-count attribute ("256 KiB" for whole-KiB values).
func attrBytes(attrs map[string]string, key string) string {
	v, ok := attrs[key]
	if !ok {
		return unknownValue
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return unknownValue
	}
	if n >= 1024 && n%1024 == 0 {
		return fmt.Sprintf("%d KiB", n/1024)
	}
	return fmt.Sprintf("%d B", n)
}

// encryptionLabel summarizes a queue's at-rest encryption from its
// attributes: a customer KMS key, SQS-managed SSE, or none. An unreadable
// attribute map never reaches here — the caller renders the whole overview as
// failed instead.
func encryptionLabel(attrs map[string]string) string {
	if key := attrs["KmsMasterKeyId"]; key != "" {
		return "KMS (" + key + ")"
	}
	if strings.EqualFold(attrs["SqsManagedSseEnabled"], "true") {
		return "SSE-SQS"
	}
	return "none"
}

// isFifo reports whether the attributes describe a FIFO queue.
func isFifo(attrs map[string]string) bool {
	return strings.EqualFold(attrs["FifoQueue"], "true")
}
