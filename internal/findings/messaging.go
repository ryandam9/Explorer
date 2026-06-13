package findings

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Messaging (SQS/SNS plumbing) check IDs (stable; see README "The checks").
const (
	CheckQueueNoConsumers = "MSG-SQS-001"
	CheckRedriveDangling  = "MSG-SQS-002"
	CheckDLQNotEmpty      = "MSG-SQS-003"
	CheckSubPending       = "MSG-SNS-001"
	CheckTopicNoSubs      = "MSG-SNS-002"
)

// MessagingSnapshot is the per-region input to AnalyzeMessaging.
type MessagingSnapshot struct {
	Region string
	Now    time.Time

	Queues []MsgQueue
	// QueuesComplete is true when the queue listing finished without error
	// or truncation. The dangling-redrive check needs the complete set:
	// deciding "target doesn't exist" from a partial listing would flag
	// healthy redrives.
	QueuesComplete bool

	Topics []MsgTopic
}

// MsgQueue is one SQS queue's plumbing posture.
type MsgQueue struct {
	Name       string
	ARN        string
	Depth      int64  // ApproximateNumberOfMessages
	NotVisible int64  // ApproximateNumberOfMessagesNotVisible (in flight)
	RedriveARN string // deadLetterTargetArn from RedrivePolicy, "" when none

	// ReceiveActivityKnown is false when the CloudWatch lookback was denied
	// or skipped — the no-consumers check then stays silent (under-warn).
	ReceiveActivityKnown bool
	// Receives is the 24h sum of NumberOfEmptyReceives +
	// NumberOfMessagesReceived: any nonzero value proves a consumer polls.
	Receives float64
}

// MsgTopic is one SNS topic's subscription posture, from topic attributes.
type MsgTopic struct {
	ARN  string
	Name string
	// Counts are tri-state: nil when GetTopicAttributes failed.
	SubscriptionsConfirmed *int
	SubscriptionsPending   *int
}

// AnalyzeMessaging runs every SQS/SNS plumbing check over the snapshot. Pure.
func AnalyzeMessaging(snap MessagingSnapshot) []Finding {
	var out []Finding
	checkQueues(snap, &out)
	checkTopics(snap, &out)
	return out
}

func checkQueues(snap MessagingSnapshot, out *[]Finding) {
	arns := make(map[string]bool, len(snap.Queues))
	dlqTargets := map[string][]string{} // DLQ ARN → queues redriving to it
	for _, q := range snap.Queues {
		if q.ARN != "" {
			arns[q.ARN] = true
		}
		if q.RedriveARN != "" {
			dlqTargets[q.RedriveARN] = append(dlqTargets[q.RedriveARN], q.Name)
		}
	}

	for _, q := range snap.Queues {
		// Messages accumulating, nothing in flight, and zero receive calls
		// in 24h: producers are filling a queue nobody consumes.
		if q.Depth > 0 && q.NotVisible == 0 && q.ReceiveActivityKnown && q.Receives == 0 {
			*out = append(*out, Finding{
				ID: CheckQueueNoConsumers, Severity: SevWarning, Service: "sqs", Region: snap.Region,
				Resource: q.Name,
				Title:    "Queue is filling with no consumers",
				Detail:   fmt.Sprintf("%d message(s) waiting, none in flight, and no receive calls in the last 24h.", q.Depth),
				Fix:      "Start (or fix) the consumer, or delete the queue if it is abandoned.",
			})
		}

		// Redrive pointing at a queue that doesn't exist: messages that
		// exceed maxReceiveCount are silently lost.
		if q.RedriveARN != "" && snap.QueuesComplete && !arns[q.RedriveARN] {
			*out = append(*out, Finding{
				ID: CheckRedriveDangling, Severity: SevCritical, Service: "sqs", Region: snap.Region,
				Resource: q.Name,
				Title:    "Redrive policy targets a nonexistent queue",
				Detail:   fmt.Sprintf("deadLetterTargetArn %s matches no queue in this region — poisoned messages will be dropped.", q.RedriveARN),
				Fix:      "Recreate the DLQ or point the redrive policy at an existing queue.",
			})
		}

		// A DLQ with messages is a backlog of failures someone should read.
		if sources := dlqTargets[q.ARN]; len(sources) > 0 && q.Depth > 0 {
			*out = append(*out, Finding{
				ID: CheckDLQNotEmpty, Severity: SevWarning, Service: "sqs", Region: snap.Region,
				Resource: q.Name,
				Title:    "Dead-letter queue has messages waiting",
				Detail: fmt.Sprintf("%d failed message(s) from %s are sitting unprocessed.",
					q.Depth, strings.Join(sources, ", ")),
				Fix: "Inspect the messages, fix the consumer bug, then redrive or purge.",
			})
		}
	}
}

func checkTopics(snap MessagingSnapshot, out *[]Finding) {
	for _, t := range snap.Topics {
		res := t.Name
		if res == "" {
			res = t.ARN
		}
		if t.SubscriptionsPending != nil && *t.SubscriptionsPending > 0 {
			*out = append(*out, Finding{
				ID: CheckSubPending, Severity: SevWarning, Service: "sns", Region: snap.Region,
				Resource: res,
				Title:    "Topic has subscriptions stuck in PendingConfirmation",
				Detail: fmt.Sprintf("%d subscription(s) never confirmed — their endpoints receive nothing. (The API does not report the request age; ignore if just created.)",
					*t.SubscriptionsPending),
				Fix: "Re-send the confirmation (or re-subscribe) and confirm from the endpoint, or remove the subscription.",
			})
		}
		if t.SubscriptionsConfirmed != nil && t.SubscriptionsPending != nil &&
			*t.SubscriptionsConfirmed == 0 && *t.SubscriptionsPending == 0 {
			*out = append(*out, Finding{
				ID: CheckTopicNoSubs, Severity: SevInfo, Service: "sns", Region: snap.Region,
				Resource: res,
				Title:    "Topic has zero subscriptions",
				Detail:   "Everything published to this topic is discarded.",
				Fix:      "Subscribe the intended consumers, or delete the topic if it is abandoned.",
			})
		}
	}
}

// ParseRedriveTarget extracts deadLetterTargetArn from an SQS RedrivePolicy
// attribute value. Pure; returns "" for empty or unparsable input.
func ParseRedriveTarget(redrivePolicyJSON string) string {
	if strings.TrimSpace(redrivePolicyJSON) == "" {
		return ""
	}
	var p struct {
		DeadLetterTargetArn string `json:"deadLetterTargetArn"`
	}
	if json.Unmarshal([]byte(redrivePolicyJSON), &p) != nil {
		return ""
	}
	return p.DeadLetterTargetArn
}
