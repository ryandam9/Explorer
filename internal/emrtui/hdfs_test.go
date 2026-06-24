package emrtui

import (
	"bytes"
	"strings"
	"testing"
)

const nnJMX = `{"beans":[
 {"name":"Hadoop:service=NameNode,name=FSNamesystemState","CapacityTotal":1000,"CapacityUsed":250,"CapacityRemaining":750,"NumLiveDataNodes":2,"NumDeadDataNodes":1,"FilesTotal":42,"BlocksTotal":17},
 {"name":"Hadoop:service=NameNode,name=FSNamesystem","MissingBlocks":1,"UnderReplicatedBlocks":3,"CorruptBlocks":0},
 {"name":"Hadoop:service=NameNode,name=NameNodeInfo","PercentUsed":25.0,"Safemode":"","Version":"3.3.6, rABC","LiveNodes":"{\"ip-10-0-0-1:9866\":{\"capacity\":500,\"usedSpace\":100,\"remaining\":400,\"numBlocks\":9,\"lastContact\":1,\"adminState\":\"In Service\"}}","DeadNodes":"{\"ip-10-0-0-9:9866\":{\"capacity\":0}}"}
]}`

func TestParseHDFS(t *testing.T) {
	s, err := parseHDFS([]byte(nnJMX))
	if err != nil {
		t.Fatal(err)
	}
	if s.CapacityTotal != 1000 || s.CapacityUsed != 250 || s.CapacityRemaining != 750 {
		t.Errorf("capacity wrong: %+v", s)
	}
	if s.LiveDataNodes != 2 || s.DeadDataNodes != 1 {
		t.Errorf("datanode counts wrong: %d live / %d dead", s.LiveDataNodes, s.DeadDataNodes)
	}
	if s.MissingBlocks != 1 || s.UnderReplicated != 3 {
		t.Errorf("block health wrong: %+v", s)
	}
	if s.PercentUsed != 25 || !strings.Contains(s.Version, "3.3.6") {
		t.Errorf("info bean wrong: %+v", s)
	}
	if s.SafemodeOn() {
		t.Error("empty Safemode should read as off")
	}
	// Two DataNodes parsed (one live In Service, one dead), sorted by name.
	if len(s.DataNodes) != 2 {
		t.Fatalf("got %d datanodes, want 2", len(s.DataNodes))
	}
	if s.DataNodes[0].Name != "ip-10-0-0-1:9866" || s.DataNodes[0].State != "In Service" {
		t.Errorf("live node wrong: %+v", s.DataNodes[0])
	}
	if s.DataNodes[1].Name != "ip-10-0-0-9:9866" || s.DataNodes[1].State != "Dead" {
		t.Errorf("dead node wrong: %+v", s.DataNodes[1])
	}
	if s.DataNodes[0].Used != 100 || s.DataNodes[0].NumBlocks != 9 {
		t.Errorf("live node fields wrong: %+v", s.DataNodes[0])
	}
}

func TestParseHDFS_SafemodeOn(t *testing.T) {
	body := `{"beans":[{"name":"Hadoop:service=NameNode,name=NameNodeInfo","Safemode":"Safe mode is ON. The reported blocks 0 needs additional 5 blocks."}]}`
	s, err := parseHDFS([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if !s.SafemodeOn() {
		t.Error("non-empty Safemode should read as ON")
	}
}

func TestParseHDFS_MissingBeansAreZeroNotError(t *testing.T) {
	// A daemon that returns beans we don't recognize must not error — it degrades.
	s, err := parseHDFS([]byte(`{"beans":[{"name":"java.lang:type=Runtime","Uptime":1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.CapacityTotal != 0 || len(s.DataNodes) != 0 {
		t.Errorf("unrecognized beans should yield zero status, got %+v", s)
	}
}

func TestParseDataNodes_EmptyAndBadInput(t *testing.T) {
	if parseDataNodes("") != nil || parseDataNodes("{}") != nil {
		t.Error("empty/none should yield nil")
	}
	if parseDataNodes("not json") != nil {
		t.Error("bad JSON should yield nil, not panic")
	}
}

func TestRenderHDFS_Formats(t *testing.T) {
	s, _ := parseHDFS([]byte(nnJMX))

	var table bytes.Buffer
	if err := RenderHDFS(&table, s, "table", false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NameNode", "live", "ip-10-0-0-1:9866", "Safe mode"} {
		if !strings.Contains(table.String(), want) {
			t.Errorf("table output missing %q:\n%s", want, table.String())
		}
	}

	var j bytes.Buffer
	if err := RenderHDFS(&j, s, "json", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(j.String(), "\"liveDataNodes\"") || !strings.Contains(j.String(), "dataNodes") {
		t.Errorf("json missing fields:\n%s", j.String())
	}

	var c bytes.Buffer
	if err := RenderHDFS(&c, s, "csv", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.String(), "ip-10-0-0-1:9866") {
		t.Errorf("csv missing datanode row:\n%s", c.String())
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{0: "0 B", 512: "512 B", 1024: "1.0 KiB", 1048576: "1.0 MiB", 1073741824: "1.0 GiB"}
	for n, want := range cases {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
