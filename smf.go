package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sort"
)

const maxFileSize = 8 * 1024 * 1024

func vlq(v uint32) []byte {
	b := []byte{byte(v & 127)}
	for v >>= 7; v > 0; v >>= 7 {
		b = append([]byte{byte(v&127) | 128}, b...)
	}
	return b
}
func readVLQ(b []byte, pos *int) (uint32, error) {
	var v uint32
	for i := 0; i < 4; i++ {
		if *pos >= len(b) {
			return 0, fmt.Errorf("truncated VLQ")
		}
		x := b[*pos]
		*pos++
		v = (v << 7) | uint32(x&127)
		if x < 128 {
			return v, nil
		}
	}
	return 0, fmt.Errorf("invalid VLQ")
}
func safeDelay(d *Dump, i int) int {
	if i == 0 {
		return 0
	}
	min := 60
	if i == 1 {
		min = 100
	}
	if i < len(d.Delays) && d.Delays[i] > min {
		return d.Delays[i]
	}
	return min
}
func encodeSMF(d *Dump) ([]byte, error) {
	if e := validateDump(d); e != nil {
		return nil, e
	}
	var t bytes.Buffer
	// 1000 ticks per quarter, tempo 1,000,000 us: each tick is 1 ms.
	t.Write([]byte{0, 0xff, 0x51, 3, 0x0f, 0x42, 0x40})
	text := []byte("Finalizer Librarian - validated RAM bulk dump; 60ms packet spacing; 100ms after header")
	t.Write([]byte{0, 0xff, 1})
	t.Write(vlq(uint32(len(text))))
	t.Write(text)
	for i, m := range d.Messages {
		delay := safeDelay(d, i)
		if delay > 60000 {
			return nil, fmt.Errorf("event gap exceeds 60 seconds")
		}
		t.Write(vlq(uint32(delay)))
		t.WriteByte(0xf0)
		t.Write(vlq(uint32(len(m) - 1)))
		t.Write(m[1:])
	}
	t.Write([]byte{0, 0xff, 0x2f, 0})
	var b bytes.Buffer
	b.WriteString("MThd")
	binary.Write(&b, binary.BigEndian, uint32(6))
	binary.Write(&b, binary.BigEndian, uint16(0))
	binary.Write(&b, binary.BigEndian, uint16(1))
	binary.Write(&b, binary.BigEndian, uint16(1000))
	b.WriteString("MTrk")
	binary.Write(&b, binary.BigEndian, uint32(t.Len()))
	b.Write(t.Bytes())
	return b.Bytes(), nil
}

type midiEvent struct {
	tick         uint64
	track, order int
	status       byte
	data         []byte
}

func decodeSMF(b []byte) (*Dump, error) {
	if len(b) > maxFileSize || len(b) < 14 || string(b[:4]) != "MThd" {
		return nil, fmt.Errorf("invalid SMF header")
	}
	hl := int(binary.BigEndian.Uint32(b[4:8]))
	if hl < 6 || hl > len(b)-8 {
		return nil, fmt.Errorf("invalid header size")
	}
	format := binary.BigEndian.Uint16(b[8:10])
	tracks := int(binary.BigEndian.Uint16(b[10:12]))
	div := binary.BigEndian.Uint16(b[12:14])
	if format > 1 || tracks < 1 || tracks > 256 || (format == 0 && tracks != 1) || div == 0 || div&0x8000 != 0 {
		return nil, fmt.Errorf("only SMF Format 0/1 with PPQN timing is supported")
	}
	pos := 8 + hl
	var events []midiEvent
	for tr := 0; tr < tracks; tr++ {
		if pos+8 > len(b) || string(b[pos:pos+4]) != "MTrk" {
			return nil, fmt.Errorf("missing track")
		}
		n := int(binary.BigEndian.Uint32(b[pos+4 : pos+8]))
		pos += 8
		if n > len(b)-pos {
			return nil, fmt.Errorf("truncated track")
		}
		data := b[pos : pos+n]
		pos += n
		i := 0
		var tick uint64
		var running byte
		ended := false
		order := 0
		for i < len(data) {
			dt, e := readVLQ(data, &i)
			if e != nil {
				return nil, e
			}
			tick += uint64(dt)
			if i >= len(data) {
				return nil, fmt.Errorf("missing MIDI event")
			}
			status := data[i]
			if status < 128 {
				if running < 0x80 || running >= 0xf0 {
					return nil, fmt.Errorf("invalid running status")
				}
				status = running
			} else {
				i++
			}
			var payload []byte
			switch {
			case status == 0xff:
				running = 0
				if i >= len(data) {
					return nil, fmt.Errorf("truncated meta event")
				}
				kind := data[i]
				i++
				size, e := readVLQ(data, &i)
				if e != nil || uint64(size) > uint64(len(data)-i) {
					return nil, fmt.Errorf("invalid meta length")
				}
				payload = data[i : i+int(size)]
				i += int(size)
				if kind == 0x2f {
					if size != 0 {
						return nil, fmt.Errorf("invalid end-of-track")
					}
					ended = true
					i = len(data)
				}
				if kind == 0x51 {
					if size != 3 {
						return nil, fmt.Errorf("invalid tempo")
					}
					events = append(events, midiEvent{tick, tr, order, 0xff, append([]byte(nil), payload...)})
				}
			case status == 0xf0 || status == 0xf7:
				running = 0
				size, e := readVLQ(data, &i)
				if e != nil || uint64(size) > uint64(len(data)-i) {
					return nil, fmt.Errorf("invalid SysEx length")
				}
				payload = data[i : i+int(size)]
				i += int(size)
				events = append(events, midiEvent{tick, tr, order, status, append([]byte(nil), payload...)})
			case status >= 0x80 && status < 0xf0:
				running = status
				size := 2
				if status&0xf0 == 0xc0 || status&0xf0 == 0xd0 {
					size = 1
				}
				if size > len(data)-i {
					return nil, fmt.Errorf("truncated channel event")
				}
				for _, v := range data[i : i+size] {
					if v >= 128 {
						return nil, fmt.Errorf("invalid channel data")
					}
				}
				i += size
			default:
				return nil, fmt.Errorf("unsupported MIDI event %02X", status)
			}
			order++
			if len(events) > 200000 {
				return nil, fmt.Errorf("too many MIDI events")
			}
		}
		if !ended {
			return nil, fmt.Errorf("missing end-of-track")
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].tick != events[j].tick {
			return events[i].tick < events[j].tick
		}
		if events[i].track != events[j].track {
			return events[i].track < events[j].track
		}
		return events[i].order < events[j].order
	})
	var c Collector
	var dump *Dump
	var wire []byte
	var times []int
	tempo := uint64(500000)
	var lastTick uint64
	var micros uint64
	for _, ev := range events {
		micros += (ev.tick - lastTick) * tempo / uint64(div)
		lastTick = ev.tick
		if micros > 24*60*60*1000000 {
			return nil, fmt.Errorf("MIDI timeline exceeds 24 hours")
		}
		if ev.status == 0xff {
			tempo = uint64(ev.data[0])<<16 | uint64(ev.data[1])<<8 | uint64(ev.data[2])
			if tempo == 0 {
				return nil, fmt.Errorf("zero tempo")
			}
			continue
		}
		if ev.status == 0xf0 {
			if len(wire) > 0 {
				return nil, fmt.Errorf("unterminated SysEx")
			}
			wire = []byte{0xf0}
		} else if len(wire) == 0 {
			if len(ev.data) > 0 && ev.data[0] == 0xf0 {
				wire = nil
			} else {
				continue
			}
		}
		wire = append(wire, ev.data...)
		if len(wire) > 131072 {
			return nil, fmt.Errorf("SysEx too large")
		}
		if len(wire) == 0 || wire[len(wire)-1] != 0xf7 {
			continue
		}
		for _, v := range wire[1 : len(wire)-1] {
			if v >= 128 {
				return nil, fmt.Errorf("invalid SysEx data")
			}
		}
		before := len(c.messages)
		out, e := c.Push(wire)
		if e != nil {
			return nil, e
		}
		if len(c.messages) > before {
			times = append(times, int(micros/1000))
		}
		if out != nil {
			dump = out
		}
		wire = nil
	}
	if len(wire) > 0 {
		return nil, fmt.Errorf("unterminated SysEx")
	}
	if dump == nil {
		return nil, fmt.Errorf("complete Finalizer bulk dump not found")
	}
	for i := 1; i < len(times); i++ {
		dump.Delays[i] = times[i] - times[i-1]
	}
	if e := validateDump(dump); e != nil {
		return nil, e
	}
	return dump, nil
}
func importLegacy(b []byte) (*Dump, error) {
	var p struct {
		Format string `json:"format"`
		Data   string `json:"bulk_payload_base64"`
		Device int    `json:"bulk_device_byte"`
	}
	if e := json.Unmarshal(b, &p); e != nil {
		return nil, e
	}
	if p.Format != "Finalizer96KProject" || p.Device < 0 || p.Device > 127 {
		return nil, fmt.Errorf("invalid legacy project")
	}
	payload, e := base64.StdEncoding.DecodeString(p.Data)
	if e != nil {
		return nil, e
	}
	messages, e := buildBulkMessages(payload, byte(p.Device))
	if e != nil {
		return nil, e
	}
	var c Collector
	var d *Dump
	for _, m := range messages {
		d, e = c.Push(m)
		if e != nil {
			return nil, e
		}
	}
	if d == nil {
		return nil, fmt.Errorf("incomplete legacy bank")
	}
	return d, nil
}
