package replay

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func gz(s string) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.Bytes()
}

const sample = `{"session_id":"s-1","distinct_id":"u1","start":1,"end":2,"events":[
 {"type":4,"timestamp":1700000000000,"data":{"href":"https://shop.example.com/cart?token=secret#x","width":1,"height":1}},
 {"type":2,"timestamp":1700000000010,"data":{}},
 {"type":5,"timestamp":1700000001000,"data":{"tag":"pulse","payload":{"kind":"error","trace_id":"0123456789abcdef0123456789abcdef","message":"boom"}}},
 {"type":5,"timestamp":1700000001500,"data":{"tag":"pulse","payload":{"kind":"request","trace_id":"fedcba9876543210fedcba9876543210"}}},
 {"type":5,"timestamp":1700000001600,"data":{"tag":"other","payload":{"kind":"error"}}},
 {"type":4,"timestamp":1700000002000,"data":{"href":"https://shop.example.com/pay"}},
 {"type":3,"timestamp":1700000005000,"data":{}}]}`

func TestDecodeAndSummarise(t *testing.T) {
	for _, body := range [][]byte{[]byte(sample), gz(sample)} {
		c, err := Decode(body, body[0] != '{')
		if err != nil {
			t.Fatal(err)
		}
		m := Summarise(7, c)
		if m.ProjectID != 7 || m.SessionID != "s-1" || m.DistinctID != "u1" || m.Events != 7 || m.Pages != 2 || m.Errors != 1 {
			t.Errorf("meta: %+v", m)
		}
		if m.Start.UnixMilli() != 1700000000000 || m.End.UnixMilli() != 1700000005000 {
			t.Errorf("times from events: %v %v", m.Start, m.End)
		}
		if strings.Join(m.URLs, ",") != "https://shop.example.com/cart,https://shop.example.com/pay" {
			t.Errorf("urls must drop query strings: %v", m.URLs)
		}
		if len(m.TraceIDs) != 2 {
			t.Errorf("trace ids: %v", m.TraceIDs)
		}
		if !HasFullSnapshot(c) {
			t.Error("full snapshot")
		}
	}
}

func TestDecodeRejects(t *testing.T) {
	bomb := `{"session_id":"s","events":[{"type":3,"timestamp":1,"data":"` + strings.Repeat("a", MaxDecompressed) + `"}]}`
	cases := map[string]struct {
		body []byte
		gz   bool
	}{
		"not json":     {[]byte("nope"), false},
		"not gzip":     {[]byte(sample), true},
		"no session":   {[]byte(`{"events":[{}]}`), false},
		"bad session":  {[]byte(`{"session_id":"../../etc","events":[{}]}`), false},
		"no events":    {[]byte(`{"session_id":"s","events":[]}`), false},
		"gzip too big": {gz(bomb), true},
	}
	for name, c := range cases {
		var inv *ErrInvalid
		if _, err := Decode(c.body, c.gz); !errors.As(err, &inv) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestObjectKeysSortByTime(t *testing.T) {
	a := ObjectKey(1, "s", time.UnixMilli(999), "ff")
	b := ObjectKey(1, "s", time.UnixMilli(1000), "00")
	if !(a < b) || !strings.HasPrefix(a, "replays/1/s/") {
		t.Errorf("%s %s", a, b)
	}
}

type memObjects map[string][]byte

func (m memObjects) Put(_ context.Context, k string, d []byte) error { m[k] = d; return nil }

type memMeta []Meta

func (m *memMeta) InsertReplayChunk(_ context.Context, x Meta) error { *m = append(*m, x); return nil }

func TestWriterStoresEventsThenMeta(t *testing.T) {
	objs, meta := memObjects{}, &memMeta{}
	c, _ := Decode([]byte(sample), false)
	if err := (&Writer{Objects: objs, Meta: meta}).Store(context.Background(), 3, c); err != nil {
		t.Fatal(err)
	}
	if len(*meta) != 1 || len(objs) != 1 {
		t.Fatalf("%d %d", len(*meta), len(objs))
	}
	m := (*meta)[0]
	data := objs[m.Key]
	if data == nil || m.Bytes != uint32(len(data)) || !strings.HasPrefix(m.Key, "replays/3/s-1/1700000000000-") {
		t.Fatalf("key %s bytes %d", m.Key, m.Bytes)
	}
	zr, _ := gzip.NewReader(bytes.NewReader(data))
	var events []json.RawMessage
	if err := json.NewDecoder(zr).Decode(&events); err != nil || len(events) != 7 {
		t.Errorf("stored events: %d %v", len(events), err)
	}
}
