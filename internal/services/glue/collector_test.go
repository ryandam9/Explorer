package glue

import "testing"

func TestMetadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "glue" || c.IsGlobal() {
		t.Errorf("Name=%q Global=%v", c.Name(), c.IsGlobal())
	}
}

func TestARN(t *testing.T) {
	if got := arn("us-east-1", "123456789012", "job/etl"); got != "arn:aws:glue:us-east-1:123456789012:job/etl" {
		t.Errorf("arn = %q", got)
	}
	if got := arn("eu-west-1", "1", "database/sales"); got != "arn:aws:glue:eu-west-1:1:database/sales" {
		t.Errorf("arn = %q", got)
	}
}
