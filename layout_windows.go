//go:build windows

package main

import (
	"fmt"
	"unicode/utf16"
	"unsafe"
)

var hOutLabel, hInLabel, hOutChLabel, hInChLabel, hHardwareHint uintptr
var procMonitorFromWindow = user32.NewProc("MonitorFromWindow")
var procGetMonitorInfoW = user32.NewProc("GetMonitorInfoW")
var procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
var procGetDC = user32.NewProc("GetDC")
var procReleaseDC = user32.NewProc("ReleaseDC")
var procSelectObject = gdi32.NewProc("SelectObject")
var procGetTextExtentPoint32W = gdi32.NewProc("GetTextExtentPoint32W")
var procGetSystemMetrics = user32.NewProc("GetSystemMetrics")

func monitorWorkArea(hwnd uintptr) RECT {
	type monitorInfo struct {
		Size          uint32
		Monitor, Work RECT
		Flags         uint32
	}
	monitor, _, _ := procMonitorFromWindow.Call(hwnd, 2)
	info := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	ok, _, _ := procGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&info)))
	if ok != 0 {
		return info.Work
	}
	var rc RECT
	ok, _, _ = procSystemParametersInfoW.Call(0x30, 0, uintptr(unsafe.Pointer(&rc)), 0)
	if ok != 0 {
		return rc
	}
	return RECT{0, 0, 1024, 720}
}
func initialBounds(work RECT) RECT {
	w := min(int32(1160), max(1, work.Right-work.Left-32))
	h := min(int32(830), max(1, work.Bottom-work.Top-32))
	x := work.Left + (work.Right-work.Left-w)/2
	y := work.Top + (work.Bottom-work.Top-h)/2
	return RECT{x, y, x + w, y + h}
}
func textSize(dc uintptr, s string) POINT {
	text := utf16.Encode([]rune(s))
	text = append(text, 0)
	size := POINT{}
	procGetTextExtentPoint32W.Call(dc, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)-1), uintptr(unsafe.Pointer(&size)))
	return size
}
func updateListExtents() {
	if lists[0] == 0 {
		return
	}
	dc, _, _ := procGetDC.Call(hMain)
	if dc == 0 {
		return
	}
	old, _, _ := procSelectObject.Call(dc, fontUI)
	defer func() { procSelectObject.Call(dc, old); procReleaseDC.Call(hMain, dc) }()
	for col, h := range lists {
		var extent int32
		for row := 0; row < 32; row++ {
			slot := col*32 + row
			name := "<Not loaded>"
			if current != nil {
				name = current.Presets[slot].Name
			}
			if pendingNames[slot] {
				name += " *"
			}
			extent = max(extent, textSize(dc, fmt.Sprintf("%03d  %s", slot+1, name)).X+12)
		}
		procSendMessageW.Call(h, 0x194, uintptr(extent), 0) // LB_SETHORIZONTALEXTENT
	}
}
func layout(w, h int32) {
	if w <= 0 || h <= 0 {
		return
	}
	g := computeLayout(w, h)
	move := func(win uintptr, b box) {
		procMoveWindow.Call(win, uintptr(b.X), uintptr(b.Y), uintptr(b.W), uintptr(b.H), 1)
	}
	for _, v := range []struct {
		win uintptr
		b   box
	}{
		{hOutLabel, g.OutLabel}, {hOut, g.Out}, {hOutChLabel, g.OutChLabel}, {hCh, g.OutCh},
		{hInLabel, g.InLabel}, {hIn, g.In}, {hInChLabel, g.InChLabel}, {hInCh, g.InCh},
		{hConnect, g.Connect}, {hRefresh, g.Refresh}, {hReceive, g.Receive}, {hSend, g.Send},
		{hCancel, g.Cancel}, {hRename, g.Rename}, {hHardwareHint, g.Hint},
		{hSource, g.Source}, {hSummary, g.Summary}, {hStatus, g.Status},
	} {
		move(v.win, v.b)
	}
	textHeight := int32(13)
	dc, _, _ := procGetDC.Call(hMain)
	if dc != 0 {
		old, _, _ := procSelectObject.Call(dc, fontUI)
		textHeight = max(textHeight, textSize(dc, "Ag").Y)
		procSelectObject.Call(dc, old)
		procReleaseDC.Call(hMain, dc)
	}
	scroll, _, _ := procGetSystemMetrics.Call(3) // SM_CYHSCROLL
	for c, win := range lists {
		row := listRowHeight(g.Lists[c].H, textHeight, int32(scroll))
		procSendMessageW.Call(win, LB_SETITEMHEIGHT, 0, uintptr(row))
		move(win, g.Lists[c])
	}
	updateListExtents()
}
