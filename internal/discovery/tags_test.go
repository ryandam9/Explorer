package discovery

import (
	"reflect"
	"testing"
)

func TestDedupeSorted(t *testing.T) {
	got := dedupeSorted([]string{"b", "a", "b", "c", "a"})
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("dedupeSorted = %v, want %v", got, want)
	}
	if dedupeSorted(nil) != nil {
		t.Error("dedupeSorted(nil) should be nil")
	}
}
