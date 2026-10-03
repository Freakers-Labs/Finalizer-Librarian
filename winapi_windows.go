//go:build windows

package main

import (
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")
	winmm    = syscall.NewLazyDLL("winmm.dll")

	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procLoadCursorW      = user32.NewProc("LoadCursorW")
	procSendMessageW     = user32.NewProc("SendMessageW")
	procSetWindowTextW   = user32.NewProc("SetWindowTextW")
	procGetStockObject   = gdi32.NewProc("GetStockObject")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procGetClientRect    = user32.NewProc("GetClientRect")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procSetTimer         = user32.NewProc("SetTimer")
	procKillTimer        = user32.NewProc("KillTimer")
	procMessageBoxW      = user32.NewProc("MessageBoxW")
	procCreateMenu       = user32.NewProc("CreateMenu")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenuW      = user32.NewProc("AppendMenuW")
	procSetMenu          = user32.NewProc("SetMenu")

	procGetOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")
	procGetSaveFileNameW = comdlg32.NewProc("GetSaveFileNameW")

	procMidiOutGetNumDevs      = winmm.NewProc("midiOutGetNumDevs")
	procMidiOutGetDevCapsW     = winmm.NewProc("midiOutGetDevCapsW")
	procMidiOutOpen            = winmm.NewProc("midiOutOpen")
	procMidiOutClose           = winmm.NewProc("midiOutClose")
	procMidiOutShortMsg        = winmm.NewProc("midiOutShortMsg")
	procMidiOutPrepareHeader   = winmm.NewProc("midiOutPrepareHeader")
	procMidiOutUnprepareHeader = winmm.NewProc("midiOutUnprepareHeader")
	procMidiOutLongMsg         = winmm.NewProc("midiOutLongMsg")
	procMidiOutReset           = winmm.NewProc("midiOutReset")
	procMidiInGetNumDevs       = winmm.NewProc("midiInGetNumDevs")
	procMidiInGetDevCapsW      = winmm.NewProc("midiInGetDevCapsW")
	procMidiInOpen             = winmm.NewProc("midiInOpen")
	procMidiInClose            = winmm.NewProc("midiInClose")
	procMidiInStart            = winmm.NewProc("midiInStart")
	procMidiInStop             = winmm.NewProc("midiInStop")
	procMidiInReset            = winmm.NewProc("midiInReset")
	procMidiInPrepareHeader    = winmm.NewProc("midiInPrepareHeader")
	procMidiInUnprepareHeader  = winmm.NewProc("midiInUnprepareHeader")
	procMidiInAddBuffer        = winmm.NewProc("midiInAddBuffer")
)

const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_VISIBLE          = 0x10000000
	WS_CHILD            = 0x40000000
	WS_TABSTOP          = 0x00010000
	WS_BORDER           = 0x00800000
	CBS_DROPDOWNLIST    = 0x0003
	CBS_HASSTRINGS      = 0x0200
	SW_SHOW             = 5
	WM_DESTROY          = 0x0002
	WM_COMMAND          = 0x0111
	WM_KEYDOWN          = 0x0100
	WM_SETFONT          = 0x0030
	WM_CLOSE            = 0x0010
	WM_TIMER            = 0x0113
	CB_ADDSTRING        = 0x0143
	CB_GETCURSEL        = 0x0147
	CB_SETCURSEL        = 0x014E
	CB_RESETCONTENT     = 0x014B
	IDC_ARROW           = 32512
	DEFAULT_GUI_FONT    = 17
	CALLBACK_WINDOW     = 0x00010000
	MIM_LONGDATA        = 0x03C4
	MIM_LONGERROR       = 0x03C6
	VK_ESCAPE           = 0x1B
	MHDR_DONE           = 0x00000001
	MF_STRING           = 0x0000
	MF_POPUP            = 0x0010
	MB_ICONINFORMATION  = 0x00000040
	MB_ICONERROR        = 0x00000010
	OFN_OVERWRITEPROMPT = 0x00000002
	OFN_HIDEREADONLY    = 0x00000004
	OFN_PATHMUSTEXIST   = 0x00000800
	OFN_FILEMUSTEXIST   = 0x00001000
	OFN_EXPLORER        = 0x00080000
)

const (
	IDC_OUT          = 1001
	IDC_IN           = 1002
	IDC_CH           = 1003
	IDC_CONNECT      = 1004
	IDC_BULK_RECEIVE = 1008
	IDC_BULK_SEND    = 1009

	IDM_FILE_OPEN    = 2001
	IDM_FILE_SAVE    = 2002
	IDM_FILE_SAVE_AS = 2003
	IDM_FILE_EXIT    = 2004
)

type RECT struct{ Left, Top, Right, Bottom int32 }
type POINT struct{ X, Y int32 }

type WNDCLASSEXW struct {
	CbSize, Style                            uint32
	LpfnWndProc                              uintptr
	CbClsExtra, CbWndExtra                   int32
	HInstance, HIcon, HCursor, HbrBackground uintptr
	LpszMenuName, LpszClassName              *uint16
	HIconSm                                  uintptr
}
type MSG struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             POINT
	LPrivate       uint32
}

type MIDIOUTCAPSW struct {
	WMid, WPid                                 uint16
	VDriver                                    uint32
	SzPname                                    [32]uint16
	WTechnology, WVoices, WNotes, WChannelMask uint16
	DwSupport                                  uint32
}
type MIDIINCAPSW struct {
	WMid, WPid uint16
	VDriver    uint32
	SzPname    [32]uint16
	DwSupport  uint32
}
type MIDIHDR struct {
	LpData          uintptr
	DwBufferLength  uint32
	DwBytesRecorded uint32
	DwUser          uintptr
	DwFlags         uint32
	LpNext          uintptr
	Reserved        uintptr
	DwOffset        uint32
	DwReserved      [8]uintptr
}

type OPENFILENAMEW struct {
	LStructSize       uint32
	HwndOwner         uintptr
	HInstance         uintptr
	LpstrFilter       *uint16
	LpstrCustomFilter *uint16
	NMaxCustFilter    uint32
	NFilterIndex      uint32
	LpstrFile         *uint16
	NMaxFile          uint32
	LpstrFileTitle    *uint16
	NMaxFileTitle     uint32
	LpstrInitialDir   *uint16
	LpstrTitle        *uint16
	Flags             uint32
	NFileOffset       uint16
	NFileExtension    uint16
	LpstrDefExt       *uint16
	LCustData         uintptr
	LpfnHook          uintptr
	LpTemplateName    *uint16
	PvReserved        uintptr
	DwReserved        uint32
	FlagsEx           uint32
}

type SysexBuffer struct {
	Data     []byte
	Hdr      MIDIHDR
	Prepared bool
}

func u16(s string) *uint16 { return syscall.StringToUTF16Ptr(s) }
func utf16z(a []uint16) string {
	n := 0
	for n < len(a) && a[n] != 0 {
		n++
	}
	return syscall.UTF16ToString(a[:n])
}

func u16Multi(s string) []uint16 {
	r := utf16.Encode([]rune(s))
	return append(r, 0)
}

func createWindow(ex uint32, class, text string, style uint32, x, y, w, h int32, parent uintptr, id int) uintptr {
	hwnd, _, _ := procCreateWindowExW.Call(uintptr(ex), uintptr(unsafe.Pointer(u16(class))), uintptr(unsafe.Pointer(u16(text))), uintptr(style), uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, uintptr(id), 0, 0)
	if hwnd != 0 && fontUI != 0 {
		procSendMessageW.Call(hwnd, WM_SETFONT, fontUI, 1)
	}
	return hwnd
}
func setText(h uintptr, s string) {
	if h != 0 {
		procSetWindowTextW.Call(h, uintptr(unsafe.Pointer(u16(s))))
	}
}
func addCombo(h uintptr, s string) {
	procSendMessageW.Call(h, CB_ADDSTRING, 0, uintptr(unsafe.Pointer(u16(s))))
}
func comboSel(h uintptr) int {
	r, _, _ := procSendMessageW.Call(h, CB_GETCURSEL, 0, 0)
	return int(int32(r))
}
func setCombo(h uintptr, n int) { procSendMessageW.Call(h, CB_SETCURSEL, uintptr(n), 0) }
func loWord(v uintptr) uint16   { return uint16(v & 0xFFFF) }
func hiWord(v uintptr) uint16   { return uint16((v >> 16) & 0xFFFF) }
