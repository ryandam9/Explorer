package s3tui

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestBuildObjectMaps(t *testing.T) {
	mod := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	res := &ListObjectsResult{
		Prefixes: []s3types.CommonPrefix{{Prefix: aws.String("photos/2024/")}},
		Objects: []s3types.Object{
			{Key: aws.String("photos/a.jpg"), Size: aws.Int64(2048), LastModified: &mod},
			{Key: aws.String("photos/")}, // the prefix itself: skipped
		},
	}

	maps, count, size := buildObjectMaps(res, "photos/", false, true)
	if len(maps) != 3 { // "..", the dir, the file
		t.Fatalf("expected 3 rows, got %d: %v", len(maps), maps)
	}
	if maps[0]["name"] != ".." {
		t.Errorf("first row should be the up-dir entry, got %v", maps[0])
	}
	if maps[1]["name"] != "2024/" || maps[1]["type"] != "DIR" {
		t.Errorf("unexpected dir row: %v", maps[1])
	}
	if maps[2]["name"] != "a.jpg" || maps[2]["size"] != "2.0 KB" {
		t.Errorf("unexpected file row: %v", maps[2])
	}
	if count != 1 || size != 2048 {
		t.Errorf("count/size = %d/%d, want 1/2048", count, size)
	}

	// Continuation batches never repeat the up-dir entry.
	maps, _, _ = buildObjectMaps(res, "photos/", false, false)
	if maps[0]["name"] == ".." {
		t.Error("continuation batch must not include the up-dir entry")
	}
}

func TestObjectsLoadedAppendExtendsListing(t *testing.T) {
	m := &Model{
		bucketRegionCache:  map[string]string{},
		bucketDetailsCache: map[string]*BucketDetails{},
		seenBuckets:        map[string]bool{},
		state:              stateObjectList,
		sortAsc:            true,
	}
	m.initObjectTable()

	first := objectsLoadedMsg{
		maps:      []map[string]string{{"name": "a.txt", "type": "FILE", "size": "1 B"}},
		count:     1,
		size:      1,
		nextToken: aws.String("tok"),
	}
	next, _ := m.Update(first)
	m = next.(*Model)
	if m.objectsNextToken == nil {
		t.Fatal("truncated listing should record the continuation token")
	}

	more := objectsLoadedMsg{
		maps:     []map[string]string{{"name": "b.txt", "type": "FILE", "size": "2 B"}},
		count:    1,
		size:     2,
		appended: true,
	}
	next, _ = m.Update(more)
	m = next.(*Model)
	if len(m.objectMaps) != 2 {
		t.Fatalf("append should extend the listing, got %d rows", len(m.objectMaps))
	}
	if m.objCount != 2 || m.totalSize != 3 {
		t.Errorf("count/size = %d/%d, want 2/3", m.objCount, m.totalSize)
	}
	if m.objectsNextToken != nil {
		t.Error("final batch should clear the continuation token")
	}
}
