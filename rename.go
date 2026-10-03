package main

import (
	"fmt"
	"strings"
)

// renamePreset copies the bank and changes only the selected 20-byte name field.
// Parameter bytes, flags, padding, device ID and transfer timing are preserved.
func renamePreset(d *Dump, slot int, name string) (*Dump, error) {
	if err := validateDump(d); err != nil {
		return nil, err
	}
	if slot < 1 || slot > 128 || !d.Presets[slot-1].Used {
		return nil, fmt.Errorf("select an occupied preset")
	}
	name = strings.TrimRight(name, " ")
	if len(name) == 0 || len(name) > 19 {
		return nil, fmt.Errorf("use 1 to 19 printable ASCII characters")
	}
	for _, c := range []byte(name) {
		if c < 32 || c > 126 {
			return nil, fmt.Errorf("use printable ASCII characters only")
		}
	}
	pos := 134
	for i := 0; i < slot-1; i++ {
		if d.Payload[4+i] != 0 {
			pos += 170
		}
	}
	payload := append([]byte(nil), d.Payload...)
	// Recorded banks use 19 space-padded bytes followed by a NUL terminator.
	for i := 0; i < 20; i++ {
		payload[pos+i] = ' '
	}
	payload[pos+19] = 0
	copy(payload[pos:pos+19], name)
	msgs, err := buildBulkMessages(payload, d.Device)
	if err != nil {
		return nil, err
	}
	presets, err := parseBankPayload(payload)
	if err != nil {
		return nil, err
	}
	result := &Dump{Messages: msgs, Payload: payload, Device: d.Device, Presets: presets, Delays: append([]int(nil), d.Delays...)}
	return result, validateDump(result)
}

func presetListText(d *Dump, pending [128]bool) string {
	var b strings.Builder
	b.WriteString("Finalizer Librarian - RAM Preset List\r\n* = renamed, not confirmed on hardware\r\n\r\n")
	for i, p := range d.Presets {
		star := ""
		if pending[i] {
			star = " *"
		}
		fmt.Fprintf(&b, "%03d\t%s%s\r\n", i+1, p.Name, star)
	}
	return b.String()
}
