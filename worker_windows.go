//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

type Device struct {
	ID   int
	Name string
}
type Command struct {
	Kind                                 string
	Out, In, Channel, InChannel, Program int
	Messages                             [][]byte
	Delays                               []int
}
type Event struct {
	Kind, Text           string
	Out, In              []Device
	Data                 []byte
	Sent, Total, Program int
}

var workerHWND, workerOut, workerIn uintptr
var workerBuffers []SysexBuffer
var workerPin runtime.Pinner
var workerCommands = make(chan Command, 16)
var workerEncoder *json.Encoder
var workerChannel, workerInChannel byte
var workerTransfer [][]byte
var workerDelays []int
var workerPos int
var workerNext time.Time
var workerBeat time.Time
var workerReceiving bool

func emit(e Event) {
	if workerEncoder.Encode(e) != nil {
		os.Exit(0)
	}
}
func workerFail(text string) { emit(Event{Kind: "fatal", Text: text}); os.Exit(1) }
func enumerateWorker() {
	e := Event{Kind: "devices"}
	n, _, _ := procMidiOutGetNumDevs.Call()
	for i := uintptr(0); i < n; i++ {
		var c MIDIOUTCAPSW
		r, _, _ := procMidiOutGetDevCapsW.Call(i, uintptr(unsafe.Pointer(&c)), unsafe.Sizeof(c))
		if r == 0 {
			e.Out = append(e.Out, Device{int(i), utf16z(c.SzPname[:])})
		}
	}
	n, _, _ = procMidiInGetNumDevs.Call()
	for i := uintptr(0); i < n; i++ {
		var c MIDIINCAPSW
		r, _, _ := procMidiInGetDevCapsW.Call(i, uintptr(unsafe.Pointer(&c)), unsafe.Sizeof(c))
		if r == 0 {
			e.In = append(e.In, Device{int(i), utf16z(c.SzPname[:])})
		}
	}
	emit(e)
}
func closeWorkerPorts() {
	workerReceiving = false
	workerTransfer = nil
	if workerIn != 0 {
		procMidiInStop.Call(workerIn)
		procMidiInReset.Call(workerIn)
		for i := range workerBuffers {
			if workerBuffers[i].Prepared {
				r, _, _ := procMidiInUnprepareHeader.Call(workerIn, uintptr(unsafe.Pointer(&workerBuffers[i].Hdr)), unsafe.Sizeof(MIDIHDR{}))
				if r != 0 {
					workerFail(fmt.Sprintf("MIDI IN unprepare error %d", r))
				}
			}
		}
		r, _, _ := procMidiInClose.Call(workerIn)
		if r != 0 {
			workerFail(fmt.Sprintf("MIDI IN close error %d", r))
		}
		workerIn = 0
	}
	workerPin.Unpin()
	workerBuffers = nil
	if workerOut != 0 {
		procMidiOutReset.Call(workerOut)
		r, _, _ := procMidiOutClose.Call(workerOut)
		if r != 0 {
			workerFail(fmt.Sprintf("MIDI OUT close error %d", r))
		}
		workerOut = 0
	}
}
func workerConnect(c Command) {
	closeWorkerPorts()
	workerChannel = byte(c.Channel) & 15
	workerInChannel = byte(c.InChannel) & 15
	if c.Out >= 0 {
		r, _, _ := procMidiOutOpen.Call(uintptr(unsafe.Pointer(&workerOut)), uintptr(c.Out), 0, 0, 0)
		if r != 0 {
			workerFail(fmt.Sprintf("MIDI OUT open error %d", r))
		}
	}
	if c.In >= 0 {
		r, _, _ := procMidiInOpen.Call(uintptr(unsafe.Pointer(&workerIn)), uintptr(c.In), workerHWND, 0, CALLBACK_WINDOW)
		if r != 0 {
			workerFail(fmt.Sprintf("MIDI IN open error %d", r))
		}
		workerBuffers = make([]SysexBuffer, 8)
		for i := range workerBuffers {
			b := &workerBuffers[i]
			b.Data = make([]byte, 8192)
			workerPin.Pin(&b.Data[0])
			workerPin.Pin(&b.Hdr)
			b.Hdr = MIDIHDR{LpData: uintptr(unsafe.Pointer(&b.Data[0])), DwBufferLength: uint32(len(b.Data))}
			hp := uintptr(unsafe.Pointer(&b.Hdr))
			r, _, _ := procMidiInPrepareHeader.Call(workerIn, hp, unsafe.Sizeof(MIDIHDR{}))
			if r != 0 {
				workerFail(fmt.Sprintf("MIDI IN prepare error %d", r))
			}
			b.Prepared = true
			r, _, _ = procMidiInAddBuffer.Call(workerIn, hp, unsafe.Sizeof(MIDIHDR{}))
			if r != 0 {
				workerFail(fmt.Sprintf("MIDI IN buffer error %d", r))
			}
		}
		r, _, _ = procMidiInStart.Call(workerIn)
		if r != 0 {
			workerFail(fmt.Sprintf("MIDI IN start error %d", r))
		}
	}
	emit(Event{Kind: "connected"})
}
func workerLong(lp uintptr, isError bool) {
	for i := range workerBuffers {
		b := &workerBuffers[i]
		if uintptr(unsafe.Pointer(&b.Hdr)) != lp {
			continue
		}
		n := int(b.Hdr.DwBytesRecorded)
		if n > len(b.Data) {
			workerFail("MIDI IN buffer overflow")
		}
		var data []byte
		if workerReceiving && n > 0 {
			data = append([]byte(nil), b.Data[:n]...)
		}
		b.Hdr.DwBytesRecorded = 0
		if workerIn != 0 {
			r, _, _ := procMidiInAddBuffer.Call(workerIn, lp, unsafe.Sizeof(MIDIHDR{}))
			if r != 0 {
				workerFail(fmt.Sprintf("MIDI IN requeue error %d", r))
			}
		}
		if isError && workerReceiving {
			emit(Event{Kind: "rxerror", Text: "MIDI SysEx receive error"})
			workerReceiving = false
		} else if len(data) > 0 {
			emit(Event{Kind: "data", Data: data})
		}
		return
	}
}
func workerSend(data []byte) {
	if workerOut == 0 {
		workerFail("MIDI OUT is not connected")
	}
	buf := append([]byte(nil), data...)
	hdr := &MIDIHDR{LpData: uintptr(unsafe.Pointer(&buf[0])), DwBufferLength: uint32(len(buf))}
	var pin runtime.Pinner
	pin.Pin(&buf[0])
	pin.Pin(hdr)
	hp := uintptr(unsafe.Pointer(hdr))
	r, _, _ := procMidiOutPrepareHeader.Call(workerOut, hp, unsafe.Sizeof(MIDIHDR{}))
	if r != 0 {
		workerFail(fmt.Sprintf("MIDI OUT prepare error %d", r))
	}
	r, _, _ = procMidiOutLongMsg.Call(workerOut, hp, unsafe.Sizeof(MIDIHDR{}))
	if r != 0 {
		workerFail(fmt.Sprintf("MIDI OUT send error %d", r))
	}
	deadline := time.Now().Add(2 * time.Second)
	for hdr.DwFlags&MHDR_DONE == 0 {
		if time.Now().After(deadline) {
			workerFail("MIDI OUT completion timed out")
		}
		time.Sleep(time.Millisecond)
	}
	r, _, _ = procMidiOutUnprepareHeader.Call(workerOut, hp, unsafe.Sizeof(MIDIHDR{}))
	if r != 0 {
		workerFail(fmt.Sprintf("MIDI OUT unprepare error %d", r))
	}
	pin.Unpin()
	runtime.KeepAlive(buf)
	runtime.KeepAlive(hdr)
}
func workerHandle(c Command) {
	switch c.Kind {
	case "connect":
		workerConnect(c)
	case "disconnect":
		closeWorkerPorts()
		emit(Event{Kind: "disconnected"})
	case "receive":
		if workerIn == 0 {
			emit(Event{Kind: "rxerror", Text: "MIDI IN is not connected"})
		} else {
			workerReceiving = true
			emit(Event{Kind: "receiving"})
		}
	case "stopreceive":
		workerReceiving = false
	case "channel":
		workerChannel = byte(c.Channel) & 15
		workerInChannel = byte(c.InChannel) & 15
	case "program":
		if workerTransfer != nil || workerReceiving {
			return
		}
		if workerOut == 0 {
			emit(Event{Kind: "error", Text: "MIDI OUT is not connected"})
			return
		}
		if c.Program < 0 || c.Program > 127 {
			return
		}
		r, _, _ := procMidiOutShortMsg.Call(workerOut, uintptr(uint32(0xc0|workerChannel)|uint32(c.Program)<<8))
		if r != 0 {
			emit(Event{Kind: "error", Text: fmt.Sprintf("Program Change error %d", r)})
		} else {
			emit(Event{Kind: "program", Program: c.Program})
		}
	case "send":
		if workerTransfer != nil || workerReceiving {
			emit(Event{Kind: "error", Text: "MIDI is busy"})
			return
		}
		if workerOut == 0 {
			emit(Event{Kind: "error", Text: "MIDI OUT is not connected"})
			return
		}
		var col Collector
		var d *Dump
		for _, m := range c.Messages {
			v, e := col.Push(m)
			if e != nil {
				emit(Event{Kind: "error", Text: e.Error()})
				return
			}
			if v != nil {
				d = v
			}
		}
		if d == nil || validateDump(d) != nil || len(d.Messages) != len(c.Messages) {
			emit(Event{Kind: "error", Text: "invalid dump"})
			return
		}
		d.Delays = c.Delays
		workerTransfer = d.Messages
		workerDelays = make([]int, len(d.Messages))
		for i := range workerDelays {
			workerDelays[i] = safeDelay(d, i)
			if workerDelays[i] > 60000 {
				emit(Event{Kind: "error", Text: "event gap exceeds 60 seconds"})
				workerTransfer = nil
				return
			}
		}
		workerPos = 0
		procSetTimer.Call(workerHWND, 1, 10, 0)
		workerNext = time.Now()
	case "shutdown":
		closeWorkerPorts()
		procPostQuitMessage.Call(0)
	}
}
func workerWnd(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case WM_TIMER:
		for i := 0; i < 16; i++ {
			select {
			case c := <-workerCommands:
				workerHandle(c)
			default:
				i = 16
			}
		}
		now := time.Now()
		if workerTransfer != nil && !now.Before(workerNext) {
			start := time.Now()
			workerSend(workerTransfer[workerPos])
			workerPos++
			emit(Event{Kind: "progress", Sent: workerPos, Total: len(workerTransfer)})
			if workerPos == len(workerTransfer) {
				workerTransfer = nil
				procSetTimer.Call(workerHWND, 1, 50, 0)
				emit(Event{Kind: "sent"})
			} else {
				workerNext = start.Add(time.Duration(workerDelays[workerPos]) * time.Millisecond)
			}
		}
		if time.Since(workerBeat) > 500*time.Millisecond {
			workerBeat = time.Now()
			emit(Event{Kind: "beat"})
		}
		return 0
	case 0x3c3: // MIM_DATA: channel voice input; never echo it to MIDI OUT.
		statusByte := byte(lp)
		if statusByte == 0xc0|workerInChannel && !workerReceiving && workerTransfer == nil {
			emit(Event{Kind: "inputprogram", Program: int((lp >> 8) & 127)})
		}
		return 0
	case MIM_LONGDATA:
		workerLong(lp, false)
		return 0
	case MIM_LONGERROR:
		workerLong(lp, true)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}
func runWorker() {
	runtime.LockOSThread()
	workerEncoder = json.NewEncoder(os.Stdout)
	inst, _, _ := procGetModuleHandleW.Call(0)
	class := u16("FinalizerLibrarianMidiWorker10")
	wc := WNDCLASSEXW{CbSize: uint32(unsafe.Sizeof(WNDCLASSEXW{})), LpfnWndProc: syscall.NewCallback(workerWnd), HInstance: inst, LpszClassName: class}
	r, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		os.Exit(1)
	}
	workerHWND, _, _ = procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, 0, 0, inst, 0)
	if workerHWND == 0 {
		os.Exit(1)
	}
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 4096), 2*1024*1024)
		for scanner.Scan() {
			var c Command
			if json.Unmarshal(scanner.Bytes(), &c) != nil {
				os.Exit(1)
			}
			select {
			case workerCommands <- c:
			default:
				os.Exit(1)
			}
		}
		os.Exit(0)
	}()
	enumerateWorker()
	procSetTimer.Call(workerHWND, 1, 50, 0)
	var m MSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}
