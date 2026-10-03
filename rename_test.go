package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenameSparseBankPreservesParameters(t *testing.T) {
	// Three noncontiguous slots; third record's name crosses a 64-byte packet boundary.
	d := sample(t, 3)
	p := append([]byte(nil), d.Payload...)
	for i := 4; i < 132; i++ {
		p[i] = 0
	}
	p[4] = 1
	p[4+63] = 2
	p[4+127] = 1
	for i := 134; i < len(p); i++ {
		p[i] = byte(i * 37)
	}
	for i := 0; i < 3; i++ {
		copy(p[134+i*170:], []byte("Original           \x00"))
	}
	msgs, _ := buildBulkMessages(p, d.Device)
	var c Collector
	for _, m := range msgs {
		d, _ = c.Push(m)
	}
	original := append([]byte(nil), d.Payload...)
	for _, tt := range []struct{ slot, pos int }{{1, 134}, {64, 304}, {128, 474}} {
		got, e := renamePreset(d, tt.slot, "Renamed preset")
		if e != nil {
			t.Fatal(e)
		}
		for i, v := range got.Payload {
			if i >= tt.pos && i < tt.pos+20 {
				continue
			}
			if v != original[i] {
				t.Fatalf("changed non-name byte %d", i)
			}
		}
		if !bytes.Equal(d.Payload, original) {
			t.Fatal("mutated source bank")
		}
		if got.Payload[tt.pos+19] != 0 {
			t.Fatal("missing terminator")
		}
		if got.Presets[tt.slot-1].Name != "Renamed preset" {
			t.Fatal("wrong slot")
		}
		if e = validateDump(got); e != nil {
			t.Fatal(e)
		}
		b, e := encodeSMF(got)
		if e != nil {
			t.Fatal(e)
		}
		reopened, e := decodeSMF(b)
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(got.Payload, reopened.Payload) {
			t.Fatal("SMF changed renamed bank")
		}
		for i, m := range got.Messages {
			if !bytes.Equal(m, reopened.Messages[i]) {
				t.Fatal("wire mismatch")
			}
		}
		if got.Device != d.Device || len(got.Delays) != len(d.Delays) {
			t.Fatal("transfer metadata changed")
		}
		for i, v := range got.Delays {
			if v != d.Delays[i] {
				t.Fatal("timing changed")
			}
		}
	}
}
func TestRenameValidationAndExport(t *testing.T) {
	d := sample(t, 1)
	for _, name := range []string{"", "   ", strings.Repeat("A", 20), "new\x00name", "new\nname", "日本語"} {
		if _, e := renamePreset(d, 1, name); e == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	for _, slot := range []int{0, 2, 128, 129} {
		if _, e := renamePreset(d, slot, "Test"); e == nil {
			t.Fatal("accepted invalid/empty slot")
		}
	}
	changed, e := renamePreset(d, 1, strings.Repeat("A", 19))
	if e != nil {
		t.Fatal(e)
	}
	var pending [128]bool
	pending[0] = true
	txt := presetListText(changed, pending)
	if !strings.Contains(txt, "001\t"+strings.Repeat("A", 19)+" *\r\n") || !strings.Contains(txt, "128\t<EMPTY>\r\n") {
		t.Fatal("export content")
	}
	if strings.Count(txt, "\t") != 128 {
		t.Fatal("missing rows")
	}
}
