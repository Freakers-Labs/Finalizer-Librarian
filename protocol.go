package main

import (
	"bytes"
	"fmt"
)

type PresetInfo struct {
	Slot int
	Name string
	Used bool
}

func cleanPresetName(b []byte) string {
	end := len(b)
	for end > 0 && (b[end-1] == 0 || b[end-1] == ' ') {
		end--
	}
	if end == 0 {
		return "<EMPTY>"
	}
	out := make([]byte, end)
	for i := 0; i < end; i++ {
		c := b[i]
		if c >= 32 && c <= 126 {
			out[i] = c
		} else {
			out[i] = '?'
		}
	}
	return string(out)
}

func parseBankPayload(payload []byte) ([128]PresetInfo, error) {
	var out [128]PresetInfo
	for i := 0; i < 128; i++ {
		out[i] = PresetInfo{Slot: i + 1, Name: "<EMPTY>", Used: false}
	}
	if len(payload) < 134 {
		return out, fmt.Errorf("payload too short: %d bytes", len(payload))
	}
	if payload[0] != 0xDD || payload[1] != 0xEE || payload[2] != 0xAA || payload[3] != 0xCC {
		return out, fmt.Errorf("Finalizer bank signature not found")
	}
	flags := payload[4:132]
	used := 0
	for _, f := range flags {
		if f != 0 {
			used++
		}
	}
	required := 134 + used*170
	if len(payload) < required {
		return out, fmt.Errorf("incomplete preset data: need %d bytes, got %d", required, len(payload))
	}
	pos := 134
	for i, f := range flags {
		if f == 0 {
			continue
		}
		if pos+170 > len(payload) {
			return out, fmt.Errorf("preset %d is truncated", i+1)
		}
		rec := payload[pos : pos+170]
		out[i] = PresetInfo{Slot: i + 1, Name: cleanPresetName(rec[:20]), Used: true}
		pos += 170
	}
	return out, nil
}

func decodeBulkPacket(msg []byte) ([]byte, error) {
	if len(msg) != 137 {
		return nil, fmt.Errorf("unexpected packet length %d", len(msg))
	}
	if msg[0] != 0xF0 || msg[1] != 0x00 || msg[2] != 0x20 || msg[3] != 0x1F || msg[5] != 0x0F || msg[136] != 0xF7 {
		return nil, fmt.Errorf("invalid packet header")
	}
	b := make([]byte, 64)
	for i := 0; i < 64; i++ {
		hi, lo := msg[7+i*2], msg[8+i*2]
		if hi > 0x0F || lo > 0x0F {
			return nil, fmt.Errorf("invalid nibble data")
		}
		b[i] = (hi << 4) | lo
	}
	sum := 0
	for _, v := range b {
		sum += int(v)
	}
	want := byte((-sum) & 0x7F)
	if msg[135] != want {
		return nil, fmt.Errorf("checksum mismatch: got %02X want %02X", msg[135], want)
	}
	return b, nil
}

func buildBulkMessages(payload []byte, deviceByte byte) ([][]byte, error) {
	if len(payload) == 0 || len(payload)%64 != 0 {
		return nil, fmt.Errorf("bulk payload size must be a non-zero multiple of 64 bytes")
	}
	count := len(payload) / 64
	if count > 16383 {
		return nil, fmt.Errorf("bulk payload is too large")
	}
	msgs := make([][]byte, 0, count+1)
	msgs = append(msgs, []byte{0xF0, 0x00, 0x20, 0x1F, deviceByte & 0x7F, 0x0E, byte((count / 128) & 0x7F), byte(count & 0x7F), 0xF7})
	for i := 0; i < count; i++ {
		block := payload[i*64 : (i+1)*64]
		m := make([]byte, 0, 137)
		m = append(m, 0xF0, 0x00, 0x20, 0x1F, deviceByte&0x7F, 0x0F, byte(i%128))
		sum := 0
		for _, v := range block {
			m = append(m, (v>>4)&0x0F, v&0x0F)
			sum += int(v)
		}
		m = append(m, byte((-sum)&0x7F), 0xF7)
		msgs = append(msgs, m)
	}
	return msgs, nil
}

const maxPackets = 2048

// Dump stores a validated bank and its wire messages. Rename rebuilds both together.
type Dump struct {
	Messages [][]byte
	Delays   []int
	Payload  []byte
	Device   byte
	Presets  [128]PresetInfo
}
type Collector struct {
	messages [][]byte
	payload  []byte
	count    int
	device   byte
}

func (c *Collector) Push(msg []byte) (*Dump, error) {
	if len(msg) < 7 || !bytes.Equal(msg[:4], []byte{0xf0, 0, 0x20, 0x1f}) {
		return nil, nil
	}
	if msg[5] == 0x0e {
		if len(msg) != 9 || msg[8] != 0xf7 || msg[4] > 127 || msg[6] > 127 || msg[7] > 127 {
			return nil, fmt.Errorf("invalid bulk header")
		}
		if c.count != 0 {
			return nil, fmt.Errorf("multiple/restarted bulk dump")
		}
		c.count = int(msg[6])*128 + int(msg[7])
		if c.count < 1 || c.count > maxPackets {
			return nil, fmt.Errorf("invalid packet count")
		}
		c.device = msg[4]
		c.messages = append(c.messages, append([]byte(nil), msg...))
		return nil, nil
	}
	if msg[5] != 0x0f || c.count == 0 {
		return nil, nil
	}
	if msg[4] != c.device {
		return nil, fmt.Errorf("bulk device ID changed")
	}
	n := len(c.messages) - 1
	if n >= c.count || msg[6] != byte(n%128) {
		return nil, fmt.Errorf("bulk packet missing, duplicated or out of sequence")
	}
	block, err := decodeBulkPacket(msg)
	if err != nil {
		return nil, err
	}
	c.payload = append(c.payload, block...)
	c.messages = append(c.messages, append([]byte(nil), msg...))
	if len(c.messages)-1 != c.count {
		return nil, nil
	}
	presets, err := parseBankPayload(c.payload)
	if err != nil {
		return nil, err
	}
	d := &Dump{Messages: c.messages, Payload: c.payload, Device: c.device, Presets: presets, Delays: make([]int, len(c.messages))}
	for i := 1; i < len(d.Delays); i++ {
		d.Delays[i] = 60
	}
	d.Delays[1] = 100
	return d, nil
}
func validateDump(d *Dump) error {
	if d == nil || len(d.Messages) < 2 {
		return fmt.Errorf("no complete bulk dump")
	}
	var c Collector
	var out *Dump
	for _, m := range d.Messages {
		v, e := c.Push(m)
		if e != nil {
			return e
		}
		if v != nil {
			if out != nil {
				return fmt.Errorf("multiple dumps")
			}
			out = v
		}
	}
	if out == nil || len(c.messages) != len(d.Messages) || !bytes.Equal(out.Payload, d.Payload) {
		return fmt.Errorf("incomplete or inconsistent bulk dump")
	}
	return nil
}
