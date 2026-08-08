package sqstui

import (
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func testQueue(name, region string) Queue {
	return Queue{
		URL:    "https://sqs." + region + ".amazonaws.com/123456789012/" + name,
		Name:   name,
		Region: region,
	}
}

func testModel(queues ...Queue) *model {
	m := &model{
		width:   120,
		height:  40,
		regions: []string{"us-east-1"},
		search:  textinput.New(),
		details: map[string]QueueDetail{},
		metrics: map[string]metricsEntry{},
		queues:  queues,
	}
	m.filterQueues()
	return m
}

func testMessage(id, body string, attrs map[string]string) types.Message {
	return types.Message{
		MessageId:  aws.String(id),
		Body:       aws.String(body),
		Attributes: attrs,
	}
}

func TestFilterQueues(t *testing.T) {
	m := testModel(
		testQueue("orders-queue", "us-east-1"),
		testQueue("orders-dlq", "us-east-1"),
		testQueue("jobs", "eu-west-1"),
	)

	m.search.SetValue("orders")
	m.filterQueues()
	if len(m.filtered) != 2 {
		t.Errorf("expected 2 queues matching 'orders', got %d", len(m.filtered))
	}

	// Region term matches too.
	m.search.SetValue("eu-west")
	m.filterQueues()
	if len(m.filtered) != 1 || m.filtered[0].Name != "jobs" {
		t.Errorf("expected the eu-west queue, got %+v", m.filtered)
	}
}

func TestDetailMsgCachesAndClearsLoading(t *testing.T) {
	q1 := testQueue("q1", "us-east-1")
	q2 := testQueue("q2", "us-east-1")
	m := testModel(q1, q2)
	m.selectedIdx = 0
	m.detailLoading = true

	// A response for a non-selected queue is cached but keeps the spinner.
	newModel, _ := m.Update(detailMsg{key: detailKey(q2), detail: QueueDetail{Attrs: map[string]string{"QueueArn": "arn2"}}})
	m2 := newModel.(*model)
	if !m2.detailLoading {
		t.Error("response for a non-selected queue must not clear the loading flag")
	}
	if _, ok := m2.details[detailKey(q2)]; !ok {
		t.Error("stale responses should still be cached")
	}

	newModel, _ = m2.Update(detailMsg{key: detailKey(q1), detail: QueueDetail{Attrs: map[string]string{"QueueArn": "arn1"}}})
	m3 := newModel.(*model)
	if m3.detailLoading {
		t.Error("selected queue's response should clear the loading flag")
	}
}

func TestPeekRequiresConfirmation(t *testing.T) {
	q := testQueue("orders-queue", "us-east-1")
	m := testModel(q)
	m.client = &Client{} // never reached: the confirm gate intercepts first

	newModel, _ := m.Update(keyMsg("P"))
	m2 := newModel.(*model)
	if !m2.peekConfirm {
		t.Fatal("P should open the peek confirmation, not peek directly")
	}
	if m2.peekLoading {
		t.Fatal("no fetch may start before the confirmation")
	}

	// Any key other than y/Enter backs out without peeking.
	newModel, _ = m2.Update(keyMsg("x"))
	m3 := newModel.(*model)
	if m3.peekConfirm || m3.peekLoading {
		t.Error("a non-confirming key must cancel the peek")
	}

	// Confirming starts the fetch.
	newModel, _ = m3.Update(keyMsg("P"))
	newModel, cmd := newModel.(*model).Update(keyMsg("y"))
	m4 := newModel.(*model)
	if !m4.peekLoading || cmd == nil {
		t.Error("y should start the peek fetch")
	}
	if m4.peekQueue.Name != "orders-queue" {
		t.Errorf("peek should pin the confirmed queue, got %q", m4.peekQueue.Name)
	}
}

func TestPeekMsgOpensMessagesView(t *testing.T) {
	q := testQueue("orders-queue", "us-east-1")
	m := testModel(q)
	m.peekQueue = q
	m.peekLoading = true

	msgs := []types.Message{
		testMessage("m1", `{"order":1}`, map[string]string{"SentTimestamp": "1700000000000", "ApproximateReceiveCount": "2"}),
		testMessage("m2", "plain text", map[string]string{"SentTimestamp": "1700000001000", "ApproximateReceiveCount": "1"}),
	}
	newModel, _ := m.Update(peekMsg{key: detailKey(q), msgs: msgs})
	m2 := newModel.(*model)
	if m2.view != viewMessages {
		t.Fatal("a successful peek should open the messages view")
	}
	if len(m2.msgTable.Rows()) != 2 {
		t.Errorf("message table should hold one row per message, got %d", len(m2.msgTable.Rows()))
	}

	// A stale peek (queue re-selected meanwhile) is dropped.
	other := testQueue("other", "us-east-1")
	m2.peekQueue = other
	newModel, _ = m2.Update(peekMsg{key: detailKey(q), msgs: msgs[:1]})
	m3 := newModel.(*model)
	if len(m3.messages) != 2 {
		t.Error("stale peek response should be dropped")
	}

	// Esc returns to the overview.
	m3.peekQueue = q
	newModel, _ = m3.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if newModel.(*model).view != viewOverview {
		t.Error("Esc should return to the overview")
	}
}

func TestPeekErrorSurfacesToast(t *testing.T) {
	q := testQueue("orders-queue", "us-east-1")
	m := testModel(q)
	m.peekQueue = q
	m.peekLoading = true

	newModel, _ := m.Update(peekMsg{key: detailKey(q), err: testingError("denied")})
	m2 := newModel.(*model)
	if m2.peekLoading {
		t.Error("peek error should clear the loading flag")
	}
	if !strings.Contains(m2.toast, "denied") {
		t.Errorf("peek failure must be surfaced, got toast %q", m2.toast)
	}
	if m2.view != viewOverview {
		t.Error("a failed peek must not open an empty messages view")
	}
}

func TestMessageRecordOpensFromMessagesView(t *testing.T) {
	q := testQueue("orders-queue", "us-east-1")
	m := testModel(q)
	m.peekQueue = q
	m.view = viewMessages
	m.messages = []types.Message{
		testMessage("m1", `{"order":1}`, map[string]string{"SentTimestamp": "1700000000000"}),
	}
	m.buildMessagesTable()

	newModel, _ := m.Update(keyMsg("v"))
	m2 := newModel.(*model)
	if !m2.recordActive {
		t.Fatal("v should open the message record view")
	}
	if !strings.Contains(m2.recordText, `"order": 1`) {
		t.Errorf("record should pretty-print the JSON body, got %q", m2.recordText)
	}

	newModel, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m3 := newModel.(*model)
	if m3.recordActive {
		t.Error("Esc should close the record view")
	}
	if m3.view != viewMessages {
		t.Error("closing the record must return to the messages view")
	}
}

func TestMetricsRefreshFloor(t *testing.T) {
	q := testQueue("orders-queue", "us-east-1")
	m := testModel(q)

	// Fresh cache: toggling metrics on must not re-fetch (paid API floor).
	m.metrics[detailKey(q)] = metricsEntry{fetchedAt: time.Now(), series: []MetricSeries{{Label: "x"}}}
	var cmds []tea.Cmd
	m.toggleMetrics(&cmds)
	if !m.metricsVisible {
		t.Error("m should show the metrics section")
	}
	if m.metricsLoading || len(cmds) != 0 {
		t.Error("a fresh cached series must not be re-fetched within the floor")
	}

	// Stale cache: toggling on re-fetches.
	m.metricsVisible = false
	m.metrics[detailKey(q)] = metricsEntry{fetchedAt: time.Now().Add(-2 * metricsRefreshFloor)}
	m.client = &Client{}
	m.toggleMetrics(&cmds)
	if !m.metricsLoading || len(cmds) == 0 {
		t.Error("a stale cached series should be re-fetched")
	}
}

func TestDLQJumpSelectsTarget(t *testing.T) {
	src := testQueue("orders-queue", "us-east-1")
	dlq := testQueue("orders-dlq", "us-east-1")
	m := testModel(src, dlq)
	// filterQueues sorts nothing — list order is construction order; find src.
	for i, q := range m.filtered {
		if q.Name == "orders-queue" {
			m.selectedIdx = i
		}
	}
	m.details[detailKey(src)] = QueueDetail{Attrs: map[string]string{
		"RedrivePolicy": `{"deadLetterTargetArn":"arn:aws:sqs:us-east-1:123456789012:orders-dlq","maxReceiveCount":"5"}`,
	}}

	var cmds []tea.Cmd
	m.jumpToDLQ(&cmds)
	if got, _ := m.selectedQueue(); got.Name != "orders-dlq" {
		t.Errorf("d should select the DLQ, got %q", got.Name)
	}
}

func TestDLQJumpWithoutRedriveExplains(t *testing.T) {
	q := testQueue("orders-queue", "us-east-1")
	m := testModel(q)
	m.details[detailKey(q)] = QueueDetail{Attrs: map[string]string{}}

	var cmds []tea.Cmd
	m.jumpToDLQ(&cmds)
	if !strings.Contains(m.toast, "no redrive policy") {
		t.Errorf("d without a redrive policy should explain itself, got %q", m.toast)
	}
}

func TestConsumersMsgOutcomes(t *testing.T) {
	q := testQueue("orders-queue", "us-east-1")
	m := testModel(q)
	m.jumpLoading = true

	// No consumers: coverage-honest toast, no jump.
	newModel, cmd := m.Update(consumersMsg{key: detailKey(q), region: "us-east-1"})
	m2 := newModel.(*model)
	if m2.jumpLoading {
		t.Error("consumers response should clear the jump flag")
	}
	if !strings.Contains(m2.toast, "No Lambda consumers") {
		t.Errorf("empty consumer list must not read as 'nothing consumes it', got %q", m2.toast)
	}
	_ = cmd

	// Error: surfaced.
	m2.jumpLoading = true
	newModel, _ = m2.Update(consumersMsg{key: detailKey(q), region: "us-east-1", err: testingError("denied")})
	if got := newModel.(*model).toast; !strings.Contains(got, "denied") {
		t.Errorf("consumer lookup failure must be surfaced, got %q", got)
	}
}

func TestHelpOverlayContent(t *testing.T) {
	m := testModel()
	m.width, m.height = 100, 40
	out := m.helpOverlay()
	for _, want := range []string{
		"Queues", "Messages", "Message record", "Everywhere",
		// Every binding must be discoverable here, not only via the
		// (eliding) status bar.
		"Peek", "dead-letter", "sparklines", "consumer", "console",
		"Export", "Re-peek", "Backspace", "receive-count",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help overlay missing %q", want)
		}
	}
}

func TestQueueARNFallsBackToURLDerivation(t *testing.T) {
	q := testQueue("orders-queue", "us-east-1")
	m := testModel(q)

	// No cached detail: derived from the URL.
	if got := m.queueARN(q); got != "arn:aws:sqs:us-east-1:123456789012:orders-queue" {
		t.Errorf("derived ARN = %q", got)
	}

	// Cached attributes win.
	m.details[detailKey(q)] = QueueDetail{Attrs: map[string]string{"QueueArn": "arn:custom"}}
	if got := m.queueARN(q); got != "arn:custom" {
		t.Errorf("attr ARN = %q", got)
	}
}

type testingError string

func (e testingError) Error() string { return string(e) }
