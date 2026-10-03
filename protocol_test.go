package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math/rand"
	"testing"
)

func sample(t *testing.T, slots int) *Dump {
	t.Helper()
	size := 134 + 170*slots
	size = (size + 63) / 64 * 64
	p := make([]byte, size)
	copy(p, []byte{0xdd, 0xee, 0xaa, 0xcc})
	for i := 0; i < slots; i++ {
		p[4+i] = 1
		copy(p[134+i*170:], []byte("Preset name"))
	}
	m, e := buildBulkMessages(p, 1)
	if e != nil {
		t.Fatal(e)
	}
	var c Collector
	var d *Dump
	for _, msg := range m {
		d, e = c.Push(msg)
		if e != nil {
			t.Fatal(e)
		}
	}
	if d == nil {
		t.Fatal("no dump")
	}
	return d
}
func TestRoundTripAll128AndSequenceWrap(t *testing.T) {
	d := sample(t, 128)
	if len(d.Messages) < 129 {
		t.Fatal("does not exercise wrap")
	}
	b, e := encodeSMF(d)
	if e != nil {
		t.Fatal(e)
	}
	got, e := decodeSMF(b)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(d.Payload, got.Payload) || len(d.Messages) != len(got.Messages) {
		t.Fatal("payload mismatch")
	}
	for i, m := range d.Messages {
		if !bytes.Equal(m, got.Messages[i]) {
			t.Fatalf("wire mismatch %d", i)
		}
	}
	for i, p := range got.Presets {
		if !p.Used || p.Slot != i+1 || p.Name != "Preset name" {
			t.Fatal("preset mismatch", i)
		}
	}
}
func TestEmptyBankRoundTrip(t *testing.T) {
	d := sample(t, 0)
	b, e := encodeSMF(d)
	if e != nil {
		t.Fatal(e)
	}
	g, e := decodeSMF(b)
	if e != nil || !bytes.Equal(g.Payload, d.Payload) {
		t.Fatal(e)
	}
}
func TestRejectCorruptMissingDuplicatePackets(t *testing.T) {
	d := sample(t, 4)
	tests := []struct {
		name     string
		messages [][]byte
	}{{"missing", append(append([][]byte{}, d.Messages[:2]...), d.Messages[3:]...)}, {"duplicate", append(append([][]byte{}, d.Messages[:2]...), d.Messages[1:]...)}, {"checksum", append([][]byte(nil), d.Messages...)}, {"ID", append([][]byte(nil), d.Messages...)}, {"restart", append(append([][]byte{}, d.Messages...), d.Messages...)}}
	tests[2].messages[1] = append([]byte(nil), d.Messages[1]...)
	tests[2].messages[1][135] ^= 1
	tests[3].messages[1] = append([]byte(nil), d.Messages[1]...)
	tests[3].messages[1][4] = 2
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := *d
			v.Messages = tt.messages
			if validateDump(&v) == nil {
				t.Fatal("accepted malformed dump")
			}
		})
	}
}
func TestTruncatedSMFAndFormat2(t *testing.T) {
	d := sample(t, 2)
	b, _ := encodeSMF(d)
	for n := 0; n < len(b); n++ {
		if _, e := decodeSMF(b[:n]); e == nil {
			t.Fatal("accepted truncation", n)
		}
	}
	b[9] = 2
	if _, e := decodeSMF(b); e == nil {
		t.Fatal("accepted format 2")
	}
}
func TestLegacyImport(t *testing.T) {
	d := sample(t, 12)
	b, _ := json.Marshal(map[string]any{"format": "Finalizer96KProject", "bulk_payload_base64": base64.StdEncoding.EncodeToString(d.Payload), "bulk_device_byte": 1, "params": map[string]int{"1": 127}})
	g, e := importLegacy(b)
	if e != nil || !bytes.Equal(g.Payload, d.Payload) {
		t.Fatal(e)
	}
}
func TestSplitSysexAndFormat1Tempo(t *testing.T) {
	d := sample(t, 5)
	var tr bytes.Buffer
	tr.Write([]byte{0, 0xff, 0x51, 3, 7, 0xa1, 0x20})
	for i, m := range d.Messages {
		dt := uint32(0)
		if i > 0 {
			dt = 100
		}
		tr.Write(vlq(dt))
		tr.WriteByte(0xf0)
		tr.Write(vlq(3))
		tr.Write(m[1:4])
		tr.WriteByte(0)
		tr.WriteByte(0xf7)
		tr.Write(vlq(uint32(len(m) - 4)))
		tr.Write(m[4:])
	}
	tr.Write([]byte{0, 0xff, 0x2f, 0})
	var b bytes.Buffer
	b.Write([]byte{'M', 'T', 'h', 'd', 0, 0, 0, 6, 0, 1, 0, 2, 3, 0xe8})
	b.WriteString("MTrk")
	binary.Write(&b, binary.BigEndian, uint32(4))
	b.Write([]byte{0, 0xff, 0x2f, 0})
	b.WriteString("MTrk")
	binary.Write(&b, binary.BigEndian, uint32(tr.Len()))
	b.Write(tr.Bytes())
	g, e := decodeSMF(b.Bytes())
	if e != nil || !bytes.Equal(g.Payload, d.Payload) {
		t.Fatal(e)
	}
	if g.Delays[1] != 50 {
		t.Fatal("tempo incorrect", g.Delays[1])
	}
}
func TestRandomMalformedInputsDoNotPanic(t *testing.T) {
	r := rand.New(rand.NewSource(19))
	for i := 0; i < 3000; i++ {
		b := make([]byte, r.Intn(2048))
		r.Read(b)
		decodeSMF(b)
	}
	d := sample(t, 3)
	valid, _ := encodeSMF(d)
	for i := 0; i < 1000; i++ {
		b := append([]byte(nil), valid...)
		b[r.Intn(len(b))] ^= byte(r.Intn(255) + 1)
		decodeSMF(b)
	}
}
