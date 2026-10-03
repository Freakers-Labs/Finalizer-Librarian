//go:build windows

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

const (
	LB_ADDSTRING         = 0x180
	LB_GETCURSEL         = 0x188
	LB_SETCURSEL         = 0x186
	LB_RESETCONTENT      = 0x184
	LB_SETITEMHEIGHT     = 0x1a0
	LBS_NOTIFY           = 1
	LBS_NOINTEGRALHEIGHT = 0x100
	WS_VSCROLL           = 0x200000
	WM_SIZE              = 5
	WM_GETMINMAXINFO     = 0x24
	ID_OPEN              = 2101
	ID_SAVE              = 2102
	ID_CANCEL            = 2103
	ID_REFRESH           = 2104
	ID_SAVEAS            = 2105
)

var procEnableWindow = user32.NewProc("EnableWindow")
var procIsDialogMessageW = user32.NewProc("IsDialogMessageW")
var procMoveWindow = user32.NewProc("MoveWindow")
var procMoveFileExW = kernel32.NewProc("MoveFileExW")
var hMain, fontUI, hOut, hIn, hCh, hInCh, hConnect, hStatus, hSource, hSummary, hReceive, hSend, hRename, hCancel, hRefresh uintptr
var lists [4]uintptr
var outDevices, inDevices []Device
var online, connecting, disconnecting, receiving, receivePending, sending, armed, loadingLists bool
var current *Dump
var dirty bool
var currentPath, sourceLabel, configDir string
var collector Collector
var rxWire []byte
var receiveStarted, receiveLast, operationStarted time.Time
var selectedSlot int
var config AppConfig

type bridge struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	commands chan Command
	events   chan Event
	done     chan struct{}
	last     time.Time
}

var midi *bridge

func newBridge() (*bridge, error) {
	exe, e := os.Executable()
	if e != nil {
		return nil, e
	}
	cmd := exec.Command(exe, "--midi-worker")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		in.Close()
		return nil, e
	}
	b := &bridge{cmd: cmd, stdin: in, commands: make(chan Command, 16), events: make(chan Event, 256), done: make(chan struct{}), last: time.Now()}
	if e = cmd.Start(); e != nil {
		in.Close()
		out.Close()
		return nil, e
	}
	go func() {
		enc := json.NewEncoder(in)
		for {
			select {
			case c := <-b.commands:
				if enc.Encode(c) != nil {
					return
				}
			case <-b.done:
				return
			}
		}
	}()
	go func() {
		defer out.Close()
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 8192), 2*1024*1024)
		for sc.Scan() {
			var ev Event
			if json.Unmarshal(sc.Bytes(), &ev) != nil {
				break
			}
			select {
			case b.events <- ev:
			case <-b.done:
				return
			}
		}
		select {
		case b.events <- Event{Kind: "exit", Text: "MIDI process stopped"}:
		case <-b.done:
		}
	}()
	go func() { cmd.Wait() }()
	return b, nil
}
func (b *bridge) post(c Command) bool {
	if b == nil {
		return false
	}
	select {
	case b.commands <- c:
		return true
	default:
		return false
	}
}
func (b *bridge) stop() {
	if b == nil {
		return
	}
	close(b.done)
	b.cmd.Process.Kill()
	b.stdin.Close()
}
func alert(text, title string, flags uintptr) uintptr {
	r, _, _ := procMessageBoxW.Call(hMain, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), flags)
	return r
}
func status(s string) { setText(hStatus, s) }
func title() {
	name := "Untitled"
	if currentPath != "" {
		name = filepath.Base(currentPath)
	}
	star := ""
	if dirty {
		star = " *"
	}
	setText(hMain, "Finalizer Librarian v1.3 — "+name+star)
}
func enable(h uintptr, b bool) {
	n := uintptr(0)
	if b {
		n = 1
	}
	procEnableWindow.Call(h, n)
}
func busy() bool {
	return receiving || receivePending || sending || armed || connecting || disconnecting
}
func controls() {
	block := busy()
	enable(hOut, !online && !block)
	enable(hIn, !online && !block)
	enable(hCh, !block)
	enable(hRefresh, !online && !block)
	enable(hRename, current != nil && selectedSlot > 0 && !block)
	enable(hInCh, !block)
	enable(hReceive, online && comboSel(hIn) > 0 && !block)
	enable(hSend, online && comboSel(hOut) > 0 && current != nil && (!block || armed))
	enable(hConnect, !block)
	enable(hCancel, block)
	for _, h := range lists {
		enable(h, !block)
	}
	if armed {
		setText(hSend, "Start Send")
	} else {
		setText(hSend, "Send")
	}
	if online {
		setText(hConnect, "Disconnect")
	} else if connecting {
		setText(hConnect, "Connecting...")
	} else {
		setText(hConnect, "Connect")
	}
}
func clearTransfer() {
	receiving = false
	receivePending = false
	sending = false
	armed = false
	connecting = false
	disconnecting = false
	collector = Collector{}
	rxWire = nil
}
func resetMidi(reason string) {
	if midi != nil {
		midi.stop()
		midi = nil
	}
	online = false
	clearTransfer()
	status(reason)
	controls()
}
func startMidi() {
	if midi != nil {
		midi.stop()
	}
	var e error
	midi, e = newBridge()
	if e != nil {
		status("Cannot start MIDI process: " + e.Error())
	}
	controls()
}
func fillDevices(h uintptr, ds []Device, name string) {
	procSendMessageW.Call(h, CB_RESETCONTENT, 0, 0)
	addCombo(h, "(None)")
	sel := 0
	for i, d := range ds {
		addCombo(h, d.Name)
		if portKey(d.Name) == name {
			sel = i + 1
		}
	}
	setCombo(h, sel)
}
func deviceID(h uintptr, ds []Device) int {
	n := comboSel(h) - 1
	if n < 0 || n >= len(ds) {
		return -1
	}
	return ds[n].ID
}
func rememberConfig() {
	config.OutKey = ""
	config.InKey = ""
	if n := comboSel(hOut) - 1; n >= 0 && n < len(outDevices) {
		config.OutKey = portKey(outDevices[n].Name)
	}
	if n := comboSel(hIn) - 1; n >= 0 && n < len(inDevices) {
		config.InKey = portKey(inDevices[n].Name)
	}
	config.Channel = comboSel(hCh) + 1
	config.InChannel = comboSel(hInCh) + 1
	b, _ := json.MarshalIndent(config, "", "  ")
	atomicWrite(filepath.Join(configDir, "settings.json"), b)
}
func connectClick() {
	if online {
		if !midi.post(Command{Kind: "disconnect"}) {
			resetMidi("Disconnected: MIDI command queue is full.")
			return
		}
		disconnecting = true
		operationStarted = time.Now()
		controls()
		return
	}
	if midi == nil {
		startMidi()
		status("Refreshing MIDI devices. Click Connect when ready.")
		return
	}
	out := deviceID(hOut, outDevices)
	in := deviceID(hIn, inDevices)
	if out < 0 && in < 0 {
		alert("Select a MIDI IN or MIDI OUT port.", "MIDI", MB_ICONINFORMATION)
		return
	}
	if !midi.post(Command{Kind: "connect", Out: out, In: in, Channel: comboSel(hCh), InChannel: comboSel(hInCh)}) {
		resetMidi("MIDI connection queue error")
		return
	}
	connecting = true
	operationStarted = time.Now()
	status("Opening MIDI ports...")
	controls()
}
func listPopulate() {
	loadingLists = true
	defer func() { loadingLists = false }()
	for c, h := range lists {
		procSendMessageW.Call(h, LB_RESETCONTENT, 0, 0)
		for r := 0; r < 32; r++ {
			slot := c*32 + r
			name := "<Not loaded>"
			if current != nil {
				name = current.Presets[slot].Name
			}
			if pendingNames[slot] {
				name += " *"
			}
			text := fmt.Sprintf("%03d  %s", slot+1, name)
			procSendMessageW.Call(h, LB_ADDSTRING, 0, uintptr(unsafe.Pointer(u16(text))))
		}
	}
	if selectedSlot > 0 {
		selectSlot(selectedSlot)
	}
	updateListExtents()
}
func summary() {
	if current == nil {
		setText(hSource, "Data: Not loaded")
		setText(hSummary, "RAM 001-128 / Receive via MEM to MIDI, or open a saved backup.")
		return
	}
	used := 0
	for _, p := range current.Presets {
		if p.Used {
			used++
		}
	}
	setText(hSource, "Data: "+sourceLabel)
	setText(hSummary, fmt.Sprintf("Used %d / 128   %d bytes   SysEx ID: %d   * = renamed, not confirmed on hardware", used, len(current.Payload), current.Device))
}
func applyDump(d *Dump, source, path string, changed bool) {
	current = d
	selectedSlot = 0
	pendingNames = nameStates[dumpKey(d)]
	sourceLabel = source
	currentPath = path
	dirty = changed
	listPopulate()
	summary()
	title()
	controls()
}
func recall(col int, send bool) {
	if loadingLists || busy() {
		return
	}
	r, _, _ := procSendMessageW.Call(lists[col], LB_GETCURSEL, 0, 0)
	row := int(int32(r))
	if row < 0 || row >= 32 {
		return
	}
	slot := col*32 + row + 1
	selectedSlot = slot
	for c, h := range lists {
		if c != col {
			procSendMessageW.Call(h, LB_SETCURSEL, ^uintptr(0), 0)
		}
	}
	controls()
	if !send {
		status(fmt.Sprintf("RAM %03d selected. Double-click to recall; Rename to edit its name.", slot))
		return
	}
	if current != nil && !current.Presets[slot-1].Used {
		status(fmt.Sprintf("RAM %03d is empty in this backup. No Program Change sent.", slot))
		return
	}
	if !online || comboSel(hOut) <= 0 {
		status(fmt.Sprintf("RAM %03d selected. Connect MIDI OUT to recall it.", slot))
		return
	}
	if midi.post(Command{Kind: "program", Program: slot - 1}) {
		status(fmt.Sprintf("RAM %03d Program Change queued", slot))
	} else {
		status("Cannot send Program Change: queue full.")
	}
}
func confirmDiscard() bool {
	if !confirmPending("continue") {
		return false
	}
	if !dirty {
		return true
	}
	r := alert("This backup has unsaved changes. Save before continuing?", "Unsaved data", 3|0x20)
	if r == 2 {
		return false
	}
	if r == 6 {
		return saveFile(false)
	}
	if r == 7 {
		return true
	}
	return false
}
func receiveClick() {
	if !confirmDiscard() {
		return
	}
	if !online || comboSel(hIn) <= 0 {
		return
	}
	collector = Collector{}
	rxWire = nil
	receivePending = true
	operationStarted = time.Now()
	receiveStarted = time.Now()
	receiveLast = receiveStarted
	if !midi.post(Command{Kind: "receive"}) {
		resetMidi("Receive queue error")
		return
	}
	status("Preparing to receive...")
	controls()
}
func cancelReceive(text string) {
	if midi != nil {
		midi.post(Command{Kind: "stopreceive"})
	}
	receiving = false
	receivePending = false
	collector = Collector{}
	rxWire = nil
	status(text)
	controls()
}
func feedData(data []byte) {
	if !receiving {
		return
	}
	for _, v := range data {
		if v >= 0xf8 {
			continue
		}
		if v == 0xf0 {
			if len(rxWire) > 0 {
				cancelReceive("Receive cancelled: a new SysEx started before the previous one ended.")
				return
			}
			rxWire = []byte{v}
			continue
		}
		if len(rxWire) == 0 {
			continue
		}
		if v >= 0x80 && v != 0xf7 {
			cancelReceive("Receive cancelled: malformed SysEx.")
			return
		}
		rxWire = append(rxWire, v)
		if len(rxWire) > 131072 {
			cancelReceive("Receive cancelled: SysEx size limit exceeded.")
			return
		}
		if v != 0xf7 {
			continue
		}
		before := len(collector.messages)
		d, e := collector.Push(rxWire)
		rxWire = nil
		if e != nil {
			cancelReceive("Cannot validate received data: " + e.Error())
			alert(e.Error(), "Bulk Receive", MB_ICONERROR)
			return
		}
		if len(collector.messages) > before {
			receiveLast = time.Now()
			status(fmt.Sprintf("Bulk Receive: %d / %d packets", len(collector.messages)-1, collector.count))
		}
		if d != nil {
			cancelReceive("")
			delete(nameStates, dumpKey(d))
			saveNameStates()
			applyDump(d, "Received from hardware - "+time.Now().Format("2006-01-02 15:04:05"), "", true)
			status("Receive complete. Use File > Save to save your backup.")
			alert("Bulk receive complete.\r\nThe preset list has been updated. Use File > Save to save a .mid backup.", "Bulk Receive", MB_ICONINFORMATION)
			return
		}
	}
}
func sendClick() {
	if !online || current == nil {
		return
	}
	if armed {
		if !midi.post(Command{Kind: "send", Messages: current.Messages, Delays: current.Delays}) {
			resetMidi("Send queue error")
			return
		}
		armed = false
		sending = true
		operationStarted = time.Now()
		status("Starting bulk send...")
		controls()
		return
	}
	if e := validateDump(current); e != nil {
		alert(e.Error(), "Bulk Send", MB_ICONERROR)
		return
	}
	alert("On the Finalizer, select [Utility] > [MIDI to MEM].\r\n\r\nAll hardware RAM slots 001-128 will be overwritten with the displayed bank.\r\nCheck that the source and destination models match.\r\n\r\nWhen the hardware is ready, click Start Send.", "Bulk Dump - Send", MB_ICONINFORMATION)
	armed = true
	status("Ready: select MIDI to MEM on the hardware, then click Start Send.")
	controls()
}
func cancelClick() {
	if receiving || receivePending {
		cancelReceive("Receive cancelled. Previous data retained.")
		return
	}
	if armed {
		armed = false
		status("Send preparation cancelled.")
		controls()
		return
	}
	if sending {
		resetMidi("Send cancelled; MIDI disconnected. Check the hardware before reconnecting.")
		return
	}
	if connecting || disconnecting {
		resetMidi("Connection cancelled. Click Connect to restart MIDI.")
	}
}
func poll() {
	if midi == nil {
		return
	}
	for i := 0; i < 256; i++ {
		var ev Event
		select {
		case ev = <-midi.events:
		default:
			i = 256
			continue
		}
		midi.last = time.Now()
		switch ev.Kind {
		case "devices":
			outDevices = ev.Out
			inDevices = ev.In
			fillDevices(hOut, outDevices, config.OutKey)
			fillDevices(hIn, inDevices, config.InKey)
			status("OFFLINE - Select MIDI ports and click Connect.")
			controls()
		case "connected":
			online = true
			connecting = false
			rememberConfig()
			status("ONLINE - Set hardware Program Bank = RAM and Offset = 0.")
			controls()
		case "disconnected":
			online = false
			disconnecting = false
			status("OFFLINE")
			controls()
		case "receiving":
			if receivePending {
				receivePending = false
				receiving = true
				receiveStarted = time.Now()
				receiveLast = receiveStarted
				controls()
				status("Bulk Receive: waiting for MEM to MIDI...")
				alert("On the Finalizer, select [Utility] > [MEM to MIDI].", "Bulk Dump - Receive", MB_ICONINFORMATION)
			}
		case "data":
			feedData(ev.Data)
		case "rxerror":
			cancelReceive(ev.Text)
			alert(ev.Text, "Bulk Receive", MB_ICONERROR)
		case "progress":
			if sending {
				status(fmt.Sprintf("Bulk Send: %d / %d packets", max(0, ev.Sent-1), max(0, ev.Total-1)))
			}
		case "sent":
			if sending {
				sending = false
				controls()
				status("Bulk Send complete - check the hardware completion display.")
				if hasPendingNames() {
					if alert("MIDI transmission complete.\r\nHas the Finalizer completed the bulk restore successfully?\r\n\r\nYes: clear the name markers. No: keep them for verification or retry.", "Confirm hardware restore", 4|0x20|0x100) == 6 {
						pendingNames = [128]bool{}
						persistPending()
						listPopulate()
					}
				} else {
					alert("MIDI transmission complete.\r\nCheck the completion display on the Finalizer.", "Bulk Send", MB_ICONINFORMATION)
				}
			}
		case "inputprogram":
			if !busy() && hEdit == 0 {
				selectSlot(ev.Program + 1)
				controls()
			}
		case "program":
			status(fmt.Sprintf("RAM %03d Program Change sent (not a hardware acknowledgement)", ev.Program+1))
		case "error":
			sending = false
			controls()
			status(ev.Text)
			alert(ev.Text, "MIDI", MB_ICONERROR)
		case "fatal", "exit":
			resetMidi("MIDI stopped: " + ev.Text + ". Current data retained.")
			return
		}
	}
	now := time.Now()
	if now.Sub(midi.last) > 5*time.Second {
		resetMidi("MIDI disconnected after 5 seconds without a response. Data retained.")
		return
	}
	if (connecting || disconnecting || receivePending) && now.Sub(operationStarted) > 5*time.Second {
		if receivePending && now.Sub(receiveStarted) <= 5*time.Second {
			return
		}
		resetMidi("MIDI operation timed out. Data retained.")
		return
	}
	if receiving {
		if collector.count > 0 && now.Sub(receiveLast) > 2500*time.Millisecond {
			cancelReceive("Receive stalled for 2.5 seconds. Cancelled; previous data retained.")
		} else if collector.count == 0 && now.Sub(receiveStarted) > 120*time.Second {
			cancelReceive("Receive cancelled after waiting 120 seconds for MEM to MIDI.")
		}
	}
}
func chooseFile(save bool) string {
	buf := make([]uint16, 32768)
	if save && currentPath != "" {
		copy(buf, utf16.Encode([]rune(currentPath)))
	}
	filter := u16Multi("MIDI Backup (*.mid)\x00*.mid\x00Legacy v0.9 Project (*.f96proj)\x00*.f96proj\x00All Files\x00*.*\x00\x00")
	if save {
		filter = u16Multi("MIDI Backup (*.mid)\x00*.mid\x00\x00")
	}
	flags := uint32(OFN_EXPLORER | OFN_PATHMUSTEXIST | OFN_HIDEREADONLY)
	name := "Open backup"
	if save {
		flags |= OFN_OVERWRITEPROMPT
		name = "Save SMF backup"
	} else {
		flags |= OFN_FILEMUSTEXIST
	}
	of := OPENFILENAMEW{LStructSize: uint32(unsafe.Sizeof(OPENFILENAMEW{})), HwndOwner: hMain, LpstrFilter: &filter[0], NFilterIndex: 1, LpstrFile: &buf[0], NMaxFile: uint32(len(buf)), LpstrTitle: u16(name), Flags: flags, LpstrDefExt: u16("mid")}
	var r uintptr
	if save {
		r, _, _ = procGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&of)))
	} else {
		r, _, _ = procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&of)))
	}
	if r == 0 {
		return ""
	}
	path := syscall.UTF16ToString(buf)
	if save && filepath.Ext(path) == "" {
		path += ".mid"
	}
	return path
}
func atomicWrite(path string, b []byte) error {
	dir := filepath.Dir(path)
	f, e := os.CreateTemp(dir, ".finalizer-*.tmp")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	check, e := os.ReadFile(tmp)
	if e != nil {
		return e
	}
	if !bytes.Equal(check, b) {
		return fmt.Errorf("file verification failed")
	}
	r, _, callErr := procMoveFileExW.Call(uintptr(unsafe.Pointer(u16(tmp))), uintptr(unsafe.Pointer(u16(path))), 0x1|0x8)
	if r == 0 {
		return callErr
	}
	return nil
}
func saveFile(as bool) bool {
	if busy() {
		return false
	}
	if current == nil {
		return false
	}
	path := currentPath
	if as || path == "" {
		path = chooseFile(true)
	}
	if path == "" {
		return false
	}
	if !strings.EqualFold(filepath.Ext(path), ".mid") {
		alert("Use the .mid file extension.", "Save", MB_ICONERROR)
		return false
	}
	b, e := encodeSMF(current)
	if e == nil {
		var check *Dump
		check, e = decodeSMF(b)
		if e == nil && !bytes.Equal(check.Payload, current.Payload) {
			e = fmt.Errorf("SMF round-trip mismatch")
		}
	}
	if e == nil {
		e = atomicWrite(path, b)
	}
	if e != nil {
		alert(e.Error(), "Save error", MB_ICONERROR)
		return false
	}
	currentPath = path
	dirty = false
	title()
	status("Saved: " + path)
	return true
}
func openFile() {
	if busy() || !confirmDiscard() {
		return
	}
	path := chooseFile(false)
	if path == "" {
		return
	}
	f, e := os.Open(path)
	if e != nil {
		alert(e.Error(), "Open error", MB_ICONERROR)
		return
	}
	b, e := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	f.Close()
	if e == nil && len(b) > maxFileSize {
		e = fmt.Errorf("file exceeds 8 MiB")
	}
	var d *Dump
	legacy := strings.EqualFold(filepath.Ext(path), ".f96proj")
	if e == nil {
		if legacy {
			d, e = importLegacy(b)
		} else {
			d, e = decodeSMF(b)
		}
	}
	if e != nil {
		alert("Cannot open file. Current data retained.\r\n"+e.Error(), "Open error", MB_ICONERROR)
		return
	}
	savePath := path
	if legacy {
		savePath = ""
	}
	applyDump(d, filepath.Base(path), savePath, legacy)
	status("File opened; nothing sent to hardware. Click Send to restore this bank.")
}
func mainWnd(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case WM_SIZE:
		if lists[0] != 0 && wp != 1 {
			finishRename(false)
			layout(int32(loWord(lp)), int32(hiWord(lp)))
		}
		return 0
	case WM_GETMINMAXINFO:
		type MM struct{ Reserved, MaxSize, MaxPos, MinTrack, MaxTrack POINT }
		mm := (*MM)(unsafe.Pointer(lp))
		work := monitorWorkArea(hwnd)
		mm.MinTrack = POINT{min(760, work.Right-work.Left), min(480, work.Bottom-work.Top)}
		return 0
	case WM_COMMAND:
		id := int(loWord(wp))
		notify := hiWord(wp)
		if id >= 3000 && id < 3004 && (notify == 1 || notify == 2) {
			if !finishRename(true) {
				return 0
			}
			recall(id-3000, notify == 2)
			return 0
		}
		if id == ID_EDIT && notify == 0x200 {
			finishRename(true)
			return 0
		}
		if hEdit != 0 && id != ID_EDIT {
			if !finishRename(true) {
				return 0
			}
		}
		switch id {
		case ID_RENAME:
			beginRename()
		case ID_EXPORT:
			exportList()
		case IDC_CONNECT:
			connectClick()
		case IDC_BULK_RECEIVE:
			receiveClick()
		case IDC_BULK_SEND:
			sendClick()
		case ID_CANCEL:
			cancelClick()
		case ID_REFRESH:
			rememberConfig()
			startMidi()
		case ID_OPEN, IDM_FILE_OPEN:
			openFile()
		case ID_SAVE, IDM_FILE_SAVE:
			saveFile(false)
		case ID_SAVEAS, IDM_FILE_SAVE_AS:
			saveFile(true)
		case IDM_FILE_EXIT:
			procPostMessageW.Call(hMain, WM_CLOSE, 0, 0)
		case IDC_CH, ID_IN_CH:
			config.Channel = comboSel(hCh) + 1
			config.InChannel = comboSel(hInCh) + 1
			if online {
				midi.post(Command{Kind: "channel", Channel: comboSel(hCh), InChannel: comboSel(hInCh)})
			}
			rememberConfig()
		}
		return 0
	case WM_TIMER:
		poll()
		return 0
	case WM_CLOSE:
		if !finishRename(true) {
			return 0
		}
		if sending {
			if alert("A transfer is in progress. Cancel it and exit?\r\nYou will need to check the hardware state.", "Exit", 4|0x20) != 6 {
				return 0
			}
		}
		if receiving || receivePending {
			cancelReceive("Receive cancelled.")
		}
		if sending || armed || connecting || disconnecting {
			resetMidi("MIDI stopped.")
		}
		if !confirmDiscard() {
			return 0
		}
		rememberConfig()
		resetMidi("")
		procDestroyWindow.Call(hwnd)
		return 0
	case WM_DESTROY:
		procKillTimer.Call(hwnd, 1)
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}
func buildUI() {
	fontUI, _, _ = procGetStockObject.Call(DEFAULT_GUI_FONT)
	label := func(text string, x, y, w int32) uintptr {
		return createWindow(0, "STATIC", text, WS_CHILD|WS_VISIBLE|0x4000, x, y, w, 22, hMain, 0)
	}
	button := func(text string, x, w int32, id int) uintptr {
		return createWindow(0, "BUTTON", text, WS_CHILD|WS_VISIBLE|WS_TABSTOP, x, 61, w, 30, hMain, id)
	}
	hOutLabel = label("MIDI OUT", 16, 20, 70)
	hOut = createWindow(0, "COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST|CBS_HASSTRINGS, 86, 16, 250, 300, hMain, IDC_OUT)
	hOutChLabel = label("CH", 343, 20, 24)
	hCh = createWindow(0, "COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST|CBS_HASSTRINGS, 370, 16, 56, 300, hMain, IDC_CH)
	hInLabel = label("MIDI IN", 443, 20, 60)
	hIn = createWindow(0, "COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST|CBS_HASSTRINGS, 503, 16, 250, 300, hMain, IDC_IN)
	hInChLabel = label("CH", 760, 20, 24)
	hInCh = createWindow(0, "COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST|CBS_HASSTRINGS, 787, 16, 56, 300, hMain, ID_IN_CH)
	for i := 1; i <= 16; i++ {
		addCombo(hCh, fmt.Sprint(i))
		addCombo(hInCh, fmt.Sprint(i))
	}
	setCombo(hCh, config.Channel-1)
	setCombo(hInCh, config.InChannel-1)
	hConnect = createWindow(0, "BUTTON", "Connect", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 856, 15, 110, 29, hMain, IDC_CONNECT)
	hRefresh = createWindow(0, "BUTTON", "Refresh", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 976, 15, 80, 29, hMain, ID_REFRESH)
	hReceive = button("Receive", 16, 104, IDC_BULK_RECEIVE)
	hSend = button("Send", 130, 104, IDC_BULK_SEND)
	hCancel = button("Cancel", 244, 80, ID_CANCEL)
	hRename = button("Rename", 364, 104, ID_RENAME)
	hHardwareHint = label("Hardware: Program Bank = RAM / Offset = 0", 610, 66, 450)
	hSource = createWindow(0, "STATIC", "", WS_CHILD|WS_VISIBLE|0x4000, 16, 102, 1100, 24, hMain, 0)
	hSummary = createWindow(0, "STATIC", "", WS_CHILD|WS_VISIBLE, 16, 130, 1100, 24, hMain, 0)
	for c := range lists {
		lists[c] = createWindow(0, "LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|WS_BORDER|WS_VSCROLL|0x100000|LBS_NOTIFY, 16+int32(c)*280, 170, 270, 520, hMain, 3000+c)
	}
	hStatus = createWindow(0, "STATIC", "Loading MIDI devices...", WS_CHILD|WS_VISIBLE, 16, 706, 1100, 40, hMain, 0)
	listPopulate()
	summary()
	controls()
	var r RECT
	procGetClientRect.Call(hMain, uintptr(unsafe.Pointer(&r)))
	layout(r.Right, r.Bottom)
	menu, _, _ := procCreateMenu.Call()
	file, _, _ := procCreatePopupMenu.Call()
	for _, v := range []struct {
		id   int
		text string
	}{{IDM_FILE_OPEN, "&Open..."}, {IDM_FILE_SAVE, "&Save"}, {IDM_FILE_SAVE_AS, "Save &As..."}, {ID_EXPORT, "Export Preset List as &Text..."}, {IDM_FILE_EXIT, "E&xit"}} {
		procAppendMenuW.Call(file, MF_STRING, uintptr(v.id), uintptr(unsafe.Pointer(u16(v.text))))
	}
	procAppendMenuW.Call(menu, MF_POPUP, file, uintptr(unsafe.Pointer(u16("&File"))))
	procSetMenu.Call(hMain, menu)
	procGetClientRect.Call(hMain, uintptr(unsafe.Pointer(&r)))
	layout(r.Right, r.Bottom)
}
func main() {
	if len(os.Args) > 1 && os.Args[1] == "--midi-worker" {
		runWorker()
		return
	}
	runtime.LockOSThread()
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	configDir = filepath.Join(base, "FinalizerLibrarian")
	os.MkdirAll(configDir, 0700)
	config.Channel = 1
	if b, e := os.ReadFile(filepath.Join(configDir, "settings.json")); e == nil {
		json.Unmarshal(b, &config)
	}
	if config.Channel < 1 || config.Channel > 16 {
		config.Channel = 1
	}
	if config.InChannel < 1 || config.InChannel > 16 {
		config.InChannel = 1
	}
	loadNameStates()
	inst, _, _ := procGetModuleHandleW.Call(0)
	class := u16("FinalizerLibrarianMain13")
	cur, _, _ := procLoadCursorW.Call(0, IDC_ARROW)
	wc := WNDCLASSEXW{CbSize: uint32(unsafe.Sizeof(WNDCLASSEXW{})), LpfnWndProc: syscall.NewCallback(mainWnd), HInstance: inst, HCursor: cur, HbrBackground: 16, LpszClassName: class}
	r, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return
	}
	start := initialBounds(monitorWorkArea(0))
	hMain, _, _ = procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(u16("Finalizer Librarian v1.3"))), WS_OVERLAPPEDWINDOW, uintptr(start.Left), uintptr(start.Top), uintptr(start.Right-start.Left), uintptr(start.Bottom-start.Top), 0, 0, inst, 0)
	if hMain == 0 {
		return
	}
	buildUI()
	title()
	procSetTimer.Call(hMain, 1, 50, 0)
	procShowWindow.Call(hMain, SW_SHOW)
	procUpdateWindow.Call(hMain)
	startMidi()
	var m MSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if hEdit != 0 && m.Hwnd == hEdit && m.Message == WM_KEYDOWN && (m.WParam == 13 || m.WParam == VK_ESCAPE) {
			finishRename(m.WParam == 13)
			continue
		}
		if m.Message == WM_KEYDOWN && m.WParam == VK_ESCAPE {
			cancelClick()
			continue
		}
		r, _, _ = procIsDialogMessageW.Call(hMain, uintptr(unsafe.Pointer(&m)))
		if r != 0 {
			continue
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}
