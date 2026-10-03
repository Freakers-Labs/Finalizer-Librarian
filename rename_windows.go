//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	ID_RENAME = 2110
	ID_EDIT   = 2111
	ID_EXPORT = 2112
	ID_IN_CH  = 2113
)

var hEdit uintptr
var editSlot int
var finishingEdit bool
var pendingNames [128]bool
var nameStates = map[string][128]bool{}
var procSetFocus = user32.NewProc("SetFocus")
var procGetWindowTextW = user32.NewProc("GetWindowTextW")
var procMapWindowPoints = user32.NewProc("MapWindowPoints")

func dumpKey(d *Dump) string { return fmt.Sprintf("%x", sha256.Sum256(d.Payload)) }
func loadNameStates() {
	b, e := os.ReadFile(filepath.Join(configDir, "rename-state.json"))
	if e == nil {
		json.Unmarshal(b, &nameStates)
	}
	if nameStates == nil {
		nameStates = map[string][128]bool{}
	}
}
func saveNameStates() bool {
	b, e := json.MarshalIndent(nameStates, "", "  ")
	if e == nil {
		e = atomicWrite(filepath.Join(configDir, "rename-state.json"), b)
	}
	if e != nil {
		alert("Cannot save local rename markers. The markers may not survive restarting the app.\r\n"+e.Error(), "State save error", MB_ICONERROR)
		return false
	}
	return true
}
func persistPending() {
	if current == nil {
		return
	}
	if hasPendingNames() {
		nameStates[dumpKey(current)] = pendingNames
	} else {
		delete(nameStates, dumpKey(current))
	}
	saveNameStates()
}
func hasPendingNames() bool {
	for _, v := range pendingNames {
		if v {
			return true
		}
	}
	return false
}
func confirmPending(action string) bool {
	if !hasPendingNames() {
		return true
	}
	return alert("Some renamed presets have not been confirmed on the hardware.\r\nContinue without sending a bulk dump?\r\n\r\nChoose No to return and use Send. Saving a .mid file does not update the hardware.", "Names not applied - "+action, 4|0x20|0x100) == 6
}
func selectSlot(slot int) {
	if slot < 1 || slot > 128 {
		return
	}
	selectedSlot = slot
	for c, h := range lists {
		row := ^uintptr(0)
		if c == (slot-1)/32 {
			row = uintptr((slot - 1) % 32)
		}
		procSendMessageW.Call(h, LB_SETCURSEL, row, 0)
	}
}
func beginRename() {
	if busy() || current == nil || selectedSlot == 0 {
		return
	}
	if !current.Presets[selectedSlot-1].Used {
		alert("Empty slots cannot be renamed.", "Rename", MB_ICONINFORMATION)
		return
	}
	if hEdit != 0 {
		return
	}
	editSlot = selectedSlot
	list := lists[(editSlot-1)/32]
	procSendMessageW.Call(list, LB_SETCURSEL, uintptr((editSlot-1)%32), 0)
	procSendMessageW.Call(list, 0x114, 6, 0) // WM_HSCROLL / SB_LEFT before placing the editor
	var rc RECT
	r, _, _ := procSendMessageW.Call(list, 0x198, uintptr((editSlot-1)%32), uintptr(unsafe.Pointer(&rc))) // LB_GETITEMRECT
	if int32(r) < 0 {
		return
	}
	var client RECT
	procGetClientRect.Call(list, uintptr(unsafe.Pointer(&client)))
	rc.Left = max(rc.Left, client.Left)
	rc.Right = min(rc.Right, client.Right)
	procMapWindowPoints.Call(list, hMain, uintptr(unsafe.Pointer(&rc)), 2)
	hEdit = createWindow(0, "EDIT", current.Presets[editSlot-1].Name, WS_CHILD|WS_VISIBLE|WS_TABSTOP|WS_BORDER|0x80, rc.Left+34, rc.Top, rc.Right-rc.Left-34, rc.Bottom-rc.Top, hMain, ID_EDIT)
	if hEdit == 0 {
		alert("Cannot create the name editor.", "Rename", MB_ICONERROR)
		return
	}
	procSendMessageW.Call(hEdit, 0xc5, 19, 0)          // EM_SETLIMITTEXT
	procSendMessageW.Call(hEdit, 0xb1, 0, ^uintptr(0)) // EM_SETSEL
	procSetFocus.Call(hEdit)
	status("Rename: 1-19 ASCII characters. Enter to apply, Esc to cancel.")
}
func finishRename(commit bool) bool {
	if hEdit == 0 || finishingEdit {
		return true
	}
	finishingEdit = true
	defer func() { finishingEdit = false }()
	if commit {
		buf := make([]uint16, 64)
		procGetWindowTextW.Call(hEdit, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		name := strings.TrimRight(syscall.UTF16ToString(buf), " ")
		if name != current.Presets[editSlot-1].Name {
			next, e := renamePreset(current, editSlot, name)
			if e != nil {
				alert(e.Error(), "Rename", MB_ICONERROR)
				procSetFocus.Call(hEdit)
				return false
			}
			current = next
			dirty = true
			pendingNames[editSlot-1] = true
			persistPending()

		}
	}
	old := hEdit
	hEdit = 0
	procDestroyWindow.Call(old)
	selectSlot(editSlot)
	listPopulate()
	title()
	controls()
	procSetFocus.Call(lists[(editSlot-1)/32])
	if commit {
		status("Name updated locally. Use File > Save for the file, Send for the hardware.")
	} else {
		status("Rename cancelled.")
	}
	return true
}
func exportList() {
	if busy() || current == nil {
		return
	}
	buf := make([]uint16, 32768)
	copy(buf, syscall.StringToUTF16("Finalizer_Presets.txt"))
	filter := u16Multi("Text Files (*.txt)\x00*.txt\x00\x00")
	of := OPENFILENAMEW{LStructSize: uint32(unsafe.Sizeof(OPENFILENAMEW{})), HwndOwner: hMain, LpstrFilter: &filter[0], NFilterIndex: 1, LpstrFile: &buf[0], NMaxFile: uint32(len(buf)), LpstrTitle: u16("Export preset list as text"), Flags: OFN_EXPLORER | OFN_PATHMUSTEXIST | OFN_HIDEREADONLY | OFN_OVERWRITEPROMPT, LpstrDefExt: u16("txt")}
	r, _, _ := procGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&of)))
	if r == 0 {
		return
	}
	path := syscall.UTF16ToString(buf)
	if !strings.EqualFold(filepath.Ext(path), ".txt") {
		alert("Use the .txt file extension.", "Export", MB_ICONERROR)
		return
	}
	if e := atomicWrite(path, []byte(presetListText(current, pendingNames))); e != nil {
		alert(e.Error(), "Export error", MB_ICONERROR)
		return
	}
	status("Preset list exported: " + path)
}
