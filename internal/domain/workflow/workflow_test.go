package workflow

import (
	"bytes"
	"encoding/json"
	"testing"
)

func testNode(key string, parents ...string) Node {
	return Node{Key: key, Type: "webhook", Config: map[string]any{"url": "https://example.com/hooks"}, DependsOn: parents}
}

func TestValidateGraph(t *testing.T) {
	cases := []struct {
		name  string
		nodes []Node
		valid bool
	}{
		{"diamond", []Node{testNode("end", "left", "right"), testNode("right", "root"), testNode("root"), testNode("left", "root")}, true},
		{"empty", nil, false}, {"cycle", []Node{testNode("a", "b"), testNode("b", "a")}, false},
		{"missing", []Node{testNode("a", "missing")}, false}, {"self", []Node{testNode("a", "a")}, false},
		{"duplicate node", []Node{testNode("a"), testNode("a")}, false},
		{"duplicate edge", []Node{testNode("a"), testNode("b", "a", "a")}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := (Definition{Nodes: c.nodes}).Validate(); (err == nil) != c.valid {
				t.Fatalf("valid=%v error=%v", c.valid, err)
			}
		})
	}
	for _, raw := range []string{"file:///etc/passwd", "https://user:password@example.com", "", "ftp://example.com"} {
		n := testNode("a")
		n.Config["url"] = raw
		if err := (Definition{Nodes: []Node{n}}).Validate(); err == nil {
			t.Fatalf("unsafe URL accepted %s", raw)
		}
	}
	n := testNode("a")
	n.Config["secret"] = "example"
	if err := (Definition{Nodes: []Node{n}}).Validate(); err == nil {
		t.Fatal("inline secret accepted")
	}
}

func TestSnapshotStableWithLargeNumbers(t *testing.T) {
	def := Definition{Nodes: []Node{testNode("a")}}
	def.Nodes[0].Config["account"] = json.Number("9007199254740993")
	raw, digest, err := def.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var decoded Definition
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	again, next, err := decoded.Snapshot()
	if err != nil || !bytes.Equal(raw, again) || digest != next {
		t.Fatal("snapshot changed on roundtrip", err)
	}
	decoded.Nodes[0].Config["url"] = "https://example.com/changed"
	_, changed, _ := decoded.Snapshot()
	if changed == digest {
		t.Fatal("digest ignored config changes")
	}
}
