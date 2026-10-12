//go:build windows

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

const (
	wsOverlappedWindow = 0x00CF0000
	wsVisible          = 0x10000000
	wsChild            = 0x40000000
	wsTabstop          = 0x10000
	esAutoHScroll      = 0x80
	wsBorder           = 0x800000
	wsExClientEdge     = 0x200
	wmCreate           = 0x0001
	wmDestroy          = 0x0002
	wmSize             = 0x0005
	wmPaint            = 0x000F
	wmClose            = 0x0010
	wmCommand          = 0x0111
	wmTimer            = 0x0113
	wmEraseBkgnd       = 0x0014
	wmCtlColorEdit     = 0x0133
	wmLButtonUp        = 0x0202
	wmMouseWheel       = 0x020A
	wmKeyDown          = 0x0100
	wmApp              = 0x8000
	dtLeft             = 0x0
	dtCenter           = 0x1
	dtRight            = 0x2
	dtVCenter          = 0x4
	dtSingle           = 0x20
	dtEndEllipsis      = 0x8000
	mbYesNo            = 0x4
	mbIconQuestion     = 0x20
	mbIconError        = 0x10
	mbIconInfo         = 0x40
	ofnFileMustExist   = 0x1000
	ofnPathMustExist   = 0x800
	ofnExplorer        = 0x80000
	emSetCueBanner     = 0x1501
	swShow             = 5
)

var (
	u32               = syscall.NewLazyDLL("user32.dll")
	g32               = syscall.NewLazyDLL("gdi32.dll")
	k32               = syscall.NewLazyDLL("kernel32.dll")
	comdlg            = syscall.NewLazyDLL("comdlg32.dll")
	shell             = syscall.NewLazyDLL("shell32.dll")
	dwm               = syscall.NewLazyDLL("dwmapi.dll")
	pRegisterClassEx  = u32.NewProc("RegisterClassExW")
	pCreateWindow     = u32.NewProc("CreateWindowExW")
	pDefWndProc       = u32.NewProc("DefWindowProcW")
	pShowWindow       = u32.NewProc("ShowWindow")
	pUpdateWindow     = u32.NewProc("UpdateWindow")
	pGetMessage       = u32.NewProc("GetMessageW")
	pTranslateMessage = u32.NewProc("TranslateMessage")
	pDispatchMessage  = u32.NewProc("DispatchMessageW")
	pPostQuitMessage  = u32.NewProc("PostQuitMessage")
	pPostMessage      = u32.NewProc("PostMessageW")
	pInvalidate       = u32.NewProc("InvalidateRect")
	pBeginPaint       = u32.NewProc("BeginPaint")
	pEndPaint         = u32.NewProc("EndPaint")
	pGetClient        = u32.NewProc("GetClientRect")
	pFillRect         = u32.NewProc("FillRect")
	pDrawText         = u32.NewProc("DrawTextW")
	pMoveWindow       = u32.NewProc("MoveWindow")
	pSendMessage      = u32.NewProc("SendMessageW")
	pSetTimer         = u32.NewProc("SetTimer")
	pMessageBox       = u32.NewProc("MessageBoxW")
	pLoadCursor       = u32.NewProc("LoadCursorW")
	pCreateBrush      = g32.NewProc("CreateSolidBrush")
	pCreatePen        = g32.NewProc("CreatePen")
	pSelectObject     = g32.NewProc("SelectObject")
	pDeleteObject     = g32.NewProc("DeleteObject")
	pRoundRect        = g32.NewProc("RoundRect")
	pSetBkMode        = g32.NewProc("SetBkMode")
	pSetBkColor       = g32.NewProc("SetBkColor")
	pSetTextColor     = g32.NewProc("SetTextColor")
	pCreateFont       = g32.NewProc("CreateFontW")
	pOpenFile         = comdlg.NewProc("GetOpenFileNameW")
	pShellExecute     = shell.NewProc("ShellExecuteW")
	pDwmAttr          = dwm.NewProc("DwmSetWindowAttribute")
)

func wptr(s string) *uint16       { return syscall.StringToUTF16Ptr(s) }
func cHex(r, g, b uint32) uintptr { return uintptr(r | (g << 8) | (b << 16)) }

var (
	bg          = cHex(12, 10, 20)
	sideBg      = cHex(17, 14, 30)
	panel       = cHex(26, 22, 42)
	panelAlt    = cHex(32, 26, 52)
	inputBg     = cHex(23, 19, 39)
	fg          = cHex(247, 245, 255)
	muted       = cHex(161, 151, 184)
	accent      = cHex(151, 89, 249)
	accentDark  = cHex(93, 52, 169)
	teal        = cHex(85, 227, 190)
	borderColor = cHex(57, 45, 79)
)

type rect struct{ Left, Top, Right, Bottom int32 }

func R(x, y, w, h int32) rect { return rect{x, y, x + w, y + h} }
func (r rect) contains(x, y int32) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}

type point struct{ X, Y int32 }
type msg struct {
	Hwnd           uintptr
	Message        uint32
	_              uint32
	Wparam, Lparam uintptr
	Time           uint32
	Pt             point
	LPrivate       uint32
}
type wndClass struct {
	Size, Style                        uint32
	WndProc                            uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	IconSm                             uintptr
}
type paintStruct struct {
	Hdc                uintptr
	Erase              int32
	RcPaint            rect
	Restore, IncUpdate int32
	Reserved           [32]byte
}
type openFileName struct {
	Size                                 uint32
	HwndOwner, Hinstance                 uintptr
	Filter, CustomFilter                 *uint16
	MaxCustom                            uint32
	FilterIndex                          uint32
	File                                 *uint16
	MaxFile                              uint32
	FileTitle                            *uint16
	MaxFileTitle                         uint32
	InitialDir, Title                    *uint16
	Flags                                uint32
	FileOffset, FileExtension            uint16
	DefExt                               *uint16
	CustData, Hook, Template, PvReserved uintptr
	DwReserved, FlagsEx                  uint32
}

type clickSpot struct {
	box    rect
	action string
}
type guiState struct {
	app                                  *App
	hwnd, searchEdit                     uintptr
	filter, search                       string
	offset, total                        int
	rows                                 []StoreEntry
	selectedCount                        int
	width, height                        int32
	clicks                               []clickSpot
	message                              string
	update                               UpdateStatus
	updateBusy                           bool
	busy                                 bool
	font, smallFont, titleFont, miniFont uintptr
	editBrush                            uintptr
	lastSearch                           string
}

var ui *guiState

func showError(s string) {
	pMessageBox.Call(0, uintptr(unsafe.Pointer(wptr(s))), uintptr(unsafe.Pointer(wptr("Astronyx Steam Manager"))), mbIconError)
}
func notify(title, text string) {
	pMessageBox.Call(ui.hwnd, uintptr(unsafe.Pointer(wptr(text))), uintptr(unsafe.Pointer(wptr(title))), mbIconInfo)
}
func ask(title, text string) bool {
	v, _, _ := pMessageBox.Call(ui.hwnd, uintptr(unsafe.Pointer(wptr(text))), uintptr(unsafe.Pointer(wptr(title))), mbYesNo|mbIconQuestion)
	return v == 6
}
func invalidate()           { pInvalidate.Call(ui.hwnd, 0, 0) }
func writeMessage(s string) { ui.message = s; invalidate() }
func selectText(hwnd uintptr) string {
	const max = 400
	buf := make([]uint16, max)
	u32.NewProc("GetWindowTextW").Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(max))
	return syscall.UTF16ToString(buf)
}
func updateResults() {
	if ui.offset < 0 {
		ui.offset = 0
	}
	rows, total := ui.app.store.Search(ui.search, ui.filter, ui.offset, 120, ui.app.selection.selected)
	if ui.offset >= total && ui.offset > 0 {
		ui.offset = 0
		rows, total = ui.app.store.Search(ui.search, ui.filter, 0, 120, ui.app.selection.selected)
	}
	ui.rows = rows
	ui.total = total
	ui.selectedCount = ui.app.selection.Count(ui.app.store)
	invalidate()
}
func runDesktop(app *App) {
	runtime.LockOSThread()
	ui = &guiState{app: app, filter: "all", message: "Pronto. O catálogo é local; adicionar não ativa jogos na Steam."}
	ui.update = UpdateStatus{Current: appVersion}
	ui.editBrush, _, _ = pCreateBrush.Call(inputBg)
	hInstance, _, _ := k32.NewProc("GetModuleHandleW").Call(0)
	wc := wndClass{Size: uint32(unsafe.Sizeof(wndClass{})), Instance: hInstance, ClassName: wptr("AstronyxSteamManagerNativeV22"), WndProc: syscall.NewCallback(wndProc)}
	wc.Cursor, _, _ = pLoadCursor.Call(0, 32512) // IDC_ARROW
	a, _, e := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	if a == 0 {
		showError("Não foi possível criar classe de janela: " + e.Error())
		return
	}
	hwnd, _, e := pCreateWindow.Call(0, uintptr(unsafe.Pointer(wc.ClassName)), uintptr(unsafe.Pointer(wptr("Astronyx Steam Manager · V2.2"))), wsOverlappedWindow|wsVisible, 70, 55, 1240, 830, 0, 0, hInstance, 0)
	if hwnd == 0 {
		showError("Não foi possível abrir o aplicativo: " + e.Error())
		return
	}
	ui.hwnd = hwnd
	dark := int32(1)
	pDwmAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
	pShowWindow.Call(hwnd, swShow)
	pUpdateWindow.Call(hwnd)
	updateResults()
	pSetTimer.Call(hwnd, 1, 60000, 0)
	go refreshUpdater()
	var m msg
	for {
		ret, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	pDeleteObject.Call(ui.editBrush)
	for _, f := range []uintptr{ui.font, ui.smallFont, ui.titleFont, ui.miniFont} {
		if f != 0 {
			pDeleteObject.Call(f)
		}
	}
}
func wndProc(hwnd uintptr, message uint32, wparam, lparam uintptr) uintptr {
	if ui == nil {
		return def(hwnd, message, wparam, lparam)
	}
	switch message {
	case wmCreate:
		ui.hwnd = hwnd
		ui.font = makeFont(17, 500)
		ui.smallFont = makeFont(15, 500)
		ui.titleFont = makeFont(28, 700)
		ui.miniFont = makeFont(13, 500)
		edit, _, _ := pCreateWindow.Call(0, uintptr(unsafe.Pointer(wptr("EDIT"))), uintptr(unsafe.Pointer(wptr(""))), wsChild|wsVisible|wsTabstop|esAutoHScroll|wsBorder, 290, 218, 560, 36, hwnd, 101, 0, 0)
		ui.searchEdit = edit
		pSendMessage.Call(edit, 0x0030, ui.font, 1) // WM_SETFONT
		banner := wptr("Buscar por nome ou App ID...")
		pSendMessage.Call(edit, emSetCueBanner, 0, uintptr(unsafe.Pointer(banner)))
		return 0
	case wmCtlColorEdit:
		pSetBkColor.Call(wparam, inputBg)
		pSetTextColor.Call(wparam, fg)
		return ui.editBrush
	case wmSize:
		ui.width = int32(uint16(lparam))
		ui.height = int32(uint16(lparam >> 16))
		placeSearch()
		invalidate()
		return 0
	case wmCommand:
		if (wparam&0xffff) == 101 && ((wparam>>16)&0xffff) == 0x300 {
			ui.search = selectText(ui.searchEdit)
			ui.offset = 0
			updateResults()
			return 0
		}
	case wmMouseWheel:
		var delta = int16((wparam >> 16) & 0xffff)
		if delta > 0 {
			ui.offset -= 3
		} else {
			ui.offset += 3
		}
		if ui.offset < 0 {
			ui.offset = 0
		}
		updateResults()
		return 0
	case wmKeyDown:
		switch wparam {
		case 0x22:
			ui.offset += 8
			updateResults()
		case 0x21:
			ui.offset -= 8
			if ui.offset < 0 {
				ui.offset = 0
			}
			updateResults()
		}
	case wmLButtonUp:
		x := int32(int16(lparam & 0xffff))
		y := int32(int16((lparam >> 16) & 0xffff))
		for _, spot := range ui.clicks {
			if spot.box.contains(x, y) {
				handleAction(spot.action)
				break
			}
		}
		return 0
	case wmTimer:
		if wparam == 1 {
			go refreshUpdater()
		}
		return 0
	case wmApp + 1: // update status
		updateMu.Lock()
		ui.update = receivedUpdate
		ui.updateBusy = false
		updateMu.Unlock()
		invalidate()
		return 0
	case wmApp + 2: // imported new ZIP
		catalogMu.Lock()
		c := catalogCandidate
		err := catalogErr
		catalogMu.Unlock()
		ui.busy = false
		if err != nil {
			writeMessage("Não foi possível importar catálogo: " + err.Error())
			notify("Importação falhou", err.Error())
		} else if c != nil {
			ui.app.store = c
			ui.offset = 0
			updateResults()
			writeMessage(fmt.Sprintf("Catálogo atualizado: %d entradas carregadas.", len(c.Entries)))
		}
		return 0
	case wmApp + 5:
		inspectionMu.Lock()
		record, e := inspectionResult, inspectionError
		inspectionMu.Unlock()
		ui.busy = false
		if e != nil {
			writeMessage("Consulta Steam: " + e.Error())
			notify("Consulta não disponível", e.Error())
		} else {
			label := "Pago ou não indicado como gratuito"
			if record.IsFree {
				label = "Gratuito"
			}
			idList := strings.Join(record.DLCIDs, ", ")
			if len(idList) > 160 {
				idList = idList[:160] + "..."
			}
			notify("Informações oficiais da Steam", fmt.Sprintf("%s\nApp ID: %s\nTipo: %s\nStatus: %s\nDLCs vinculados: %d\nIDs de DLCs: %s\n\nLicença: não verificada.\nNenhum jogo é ativado por esta consulta.", record.Name, record.AppID, record.Type, label, len(record.DLCIDs), idList))
			writeMessage("Informações públicas carregadas para App ID " + record.AppID)
		}
		return 0
	case wmApp + 4:
		importMu.Lock()
		res, err, source := lastImportResult, lastImportError, lastImportSource
		importMu.Unlock()
		ui.busy = false
		if err != nil {
			writeMessage(source + ": " + err.Error())
			notify("Importação falhou", err.Error())
		} else {
			writeMessage(fmt.Sprintf("%s: %d novos, %d duplicados, %d ignorados.", source, res.Added, res.Duplicates, res.Skipped))
		}
		invalidate()
		return 0
	case wmApp + 3:
		updateMu.Lock()
		s := updateErr
		updateMu.Unlock()
		ui.busy = false
		if s == "" {
			pPostMessage.Call(hwnd, wmClose, 0, 0)
		} else {
			writeMessage(s)
			notify("Atualização", s)
		}
		return 0
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		paint(hdc)
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmEraseBkgnd:
		return 1
	case wmClose:
		u32.NewProc("DestroyWindow").Call(hwnd)
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	return def(hwnd, message, wparam, lparam)
}
func def(hwnd uintptr, message uint32, w, l uintptr) uintptr {
	v, _, _ := pDefWndProc.Call(hwnd, uintptr(message), w, l)
	return v
}
func makeFont(size int32, weight int32) uintptr {
	f, _, _ := pCreateFont.Call(uintptr(-size), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(wptr("Segoe UI"))))
	return f
}
func fill(hdc uintptr, r rect, color uintptr) {
	b, _, _ := pCreateBrush.Call(color)
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), b)
	pDeleteObject.Call(b)
}
func round(hdc uintptr, r rect, color uintptr, rad int32) {
	b, _, _ := pCreateBrush.Call(color)
	p, _, _ := pCreatePen.Call(0, 1, color)
	oldb, _, _ := pSelectObject.Call(hdc, b)
	oldp, _, _ := pSelectObject.Call(hdc, p)
	pRoundRect.Call(hdc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(rad), uintptr(rad))
	pSelectObject.Call(hdc, oldb)
	pSelectObject.Call(hdc, oldp)
	pDeleteObject.Call(b)
	pDeleteObject.Call(p)
}
func txt(hdc uintptr, s string, r rect, color, font uintptr, align uint32) {
	if s == "" {
		return
	}
	old, _, _ := pSelectObject.Call(hdc, font)
	pSetBkMode.Call(hdc, 1)
	pSetTextColor.Call(hdc, color)
	u := syscall.StringToUTF16(s)
	pDrawText.Call(hdc, uintptr(unsafe.Pointer(&u[0])), ^uintptr(0), uintptr(unsafe.Pointer(&r)), uintptr(dtVCenter|dtSingle|dtEndEllipsis|align))
	pSelectObject.Call(hdc, old)
}
func (g *guiState) spot(r rect, action string) { g.clicks = append(g.clicks, clickSpot{r, action}) }
func (g *guiState) button(hdc uintptr, r rect, label, action string, primary bool) {
	color := panelAlt
	textColor := fg
	if primary {
		color = accent
	}
	if g.busy {
		color = borderColor
		textColor = muted
	}
	round(hdc, r, color, 13)
	txt(hdc, label, r, textColor, g.smallFont, dtCenter)
	g.spot(r, action)
}
func paint(hdc uintptr) {
	if ui.width < 1 || ui.height < 1 {
		return
	}
	w, h := ui.width, ui.height
	ui.clicks = ui.clicks[:0]
	fill(hdc, R(0, 0, w, h), bg)
	fill(hdc, R(0, 0, 226, h), sideBg)
	round(hdc, R(24, 26, 46, 46), accent, 14)
	txt(hdc, "A", R(24, 26, 46, 46), fg, ui.titleFont, dtCenter)
	txt(hdc, "ASTRONYX", R(81, 30, 138, 30), fg, ui.font, dtLeft)
	txt(hdc, "STEAM MANAGER", R(81, 53, 130, 20), muted, ui.miniFont, dtLeft)
	txt(hdc, "BIBLIOTECA", R(27, 117, 170, 20), muted, ui.miniFont, dtLeft)
	opts := []struct{ tab, name string }{{"all", "Todos os itens"}, {"game", "Jogos"}, {"dlc", "DLCs"}, {"software", "Programas"}, {"selected", "Minha coleção"}}
	for i, opt := range opts {
		y := int32(150 + i*56)
		r := R(15, y, 196, 43)
		if ui.filter == opt.tab {
			round(hdc, r, panelAlt, 11)
		}
		sq := teal
		if ui.filter != opt.tab {
			sq = muted
		}
		round(hdc, R(30, y+16, 8, 8), sq, 5)
		txt(hdc, opt.name, R(51, y+5, 150, 32), fg, ui.smallFont, dtLeft)
		ui.spot(r, "filter:"+opt.tab)
	}
	round(hdc, R(17, h-188, 194, 160), panel, 14)
	txt(hdc, "ARQUIVOS STEAM", R(31, h-176, 170, 28), fg, ui.smallFont, dtLeft)
	items, _ := ui.app.manifests.snapshot()
	txt(hdc, fmt.Sprintf("%d manifests importados", len(items)), R(31, h-144, 170, 24), muted, ui.miniFont, dtLeft)
	ui.button(hdc, R(29, h-105, 169, 37), "ABRIR PASTA", "folder", false)
	txt(hdc, "V2.2 · APP NATIVO", R(32, h-49, 173, 22), muted, ui.miniFont, dtLeft)
	left := int32(256)
	mainW := w - left - 26
	txt(hdc, "Sua biblioteca Steam, organizada.", R(left, 26, mainW-200, 38), fg, ui.titleFont, dtLeft)
	txt(hdc, "Catálogo local • importação em lote • sem navegador", R(left, 67, mainW, 24), muted, ui.smallFont, dtLeft)
	info := "ATUALIZAÇÕES NÃO CONFIGURADAS"
	if ui.update.Available {
		info = "NOVA VERSÃO " + ui.update.Latest + " DISPONÍVEL"
	} else if ui.update.Configured && ui.update.Error == "" {
		info = "APP ATUALIZADO"
	}
	statR := R(w-317, 33, 291, 35)
	round(hdc, statR, panel, 12)
	txt(hdc, info, statR, teal, ui.miniFont, dtCenter)
	ui.spot(statR, "update")
	gap := int32(13)
	cw := (mainW - 2*gap) / 3
	stat(hdc, R(left, 112, cw, 100), "NO CATÁLOGO", formatInt(len(ui.app.store.Entries)), "Registros indexados")
	stat(hdc, R(left+cw+gap, 112, cw, 100), "JOGOS + DLCs", formatInt(ui.app.store.TotalGameDLC), "Prontos para adicionar")
	stat(hdc, R(left+2*(cw+gap), 112, cw, 100), "MINHA COLEÇÃO", formatInt(ui.selectedCount), "Entradas salvas")
	txt(hdc, "PESQUISAR", R(left, 230, 180, 23), muted, ui.miniFont, dtLeft)
	// A real Win32 EDIT control is positioned inside this backdrop.
	round(hdc, R(left, 259, mainW, 51), inputBg, 12)
	kindLabel := map[string]string{"all": "Todos os registros", "game": "Jogos", "dlc": "DLCs", "software": "Programas", "selected": "Minha coleção"}[ui.filter]
	txt(hdc, kindLabel, R(left, 326, mainW-190, 32), fg, ui.font, dtLeft)
	txt(hdc, fmt.Sprintf("%s resultados", formatInt(ui.total)), R(w-197, 327, 169, 31), muted, ui.smallFont, dtRight)
	header := R(left, 374, mainW, 33)
	fill(hdc, header, panel)
	txt(hdc, "NOME DO PRODUTO", R(left+19, 375, mainW-335, 31), muted, ui.miniFont, dtLeft)
	txt(hdc, "TIPO", R(w-304, 375, 88, 31), muted, ui.miniFont, dtLeft)
	txt(hdc, "COLEÇÃO", R(w-185, 375, 120, 31), muted, ui.miniFont, dtCenter)
	contentTop := int32(409)
	endY := h - 145
	visible := int((endY - contentTop) / 49)
	if visible < 0 {
		visible = 0
	}
	if visible > len(ui.rows) {
		visible = len(ui.rows)
	}
	for i := 0; i < visible; i++ {
		entry := ui.rows[i]
		y := contentTop + int32(i*49)
		r := R(left, y, mainW, 46)
		if i%2 == 0 {
			fill(hdc, r, panel)
		} else {
			fill(hdc, r, panelAlt)
		}
		name := entry.Name
		txt(hdc, name, R(left+18, y+4, mainW-346, 39), fg, ui.smallFont, dtLeft)
		id := strconv.FormatUint(entry.ID, 10)
		// ID shown as metadata inside type column.
		kind := map[string]string{"game": "JOGO", "dlc": "DLC", "software": "APP", "hardware": "HARDWARE", "video": "VÍDEO"}[entry.Kind]
		txt(hdc, kind, R(w-303, y+4, 92, 20), muted, ui.miniFont, dtLeft)
		txt(hdc, "ID "+id, R(w-303, y+23, 94, 19), muted, ui.miniFont, dtLeft)
		chosen := ui.app.selection.selected(entry)
		pill := R(w-170, y+9, 111, 29)
		if chosen {
			round(hdc, pill, accentDark, 10)
			txt(hdc, "ADICIONADO", pill, fg, ui.miniFont, dtCenter)
		} else {
			round(hdc, pill, borderColor, 10)
			txt(hdc, "ADICIONAR +", pill, fg, ui.miniFont, dtCenter)
		}
		ui.spot(pill, "toggle:"+strconv.FormatUint(entry.ID, 10))
		ui.spot(r, "inspect:"+strconv.FormatUint(entry.ID, 10))
	}
	if ui.total == 0 {
		txt(hdc, "Nenhum registro encontrado.", R(left+15, 430, mainW-15, 35), muted, ui.font, dtLeft)
	}
	if ui.total > ui.offset+visible {
		txt(hdc, "Role a lista para ver mais registros ↓", R(left, endY+2, mainW, 25), muted, ui.miniFont, dtCenter)
	}
	fill(hdc, R(226, h-119, w-226, 119), sideBg)
	txt(hdc, ui.message, R(left, h-111, mainW, 24), muted, ui.miniFont, dtLeft)
	bottomY := h - 75
	ui.button(hdc, R(left, bottomY, 210, 47), "+ ADICIONAR TODOS", "all", true)
	ui.button(hdc, R(left+221, bottomY, 155, 47), "IMPORTAR ZIP", "catalog", false)
	ui.button(hdc, R(left+387, bottomY, 163, 47), "MANIFESTS ZIP", "manifests", false)
	if mainW > 715 {
		ui.button(hdc, R(left+561, bottomY, 150, 47), "STEAM LOCAL", "steam", false)
	}
	if ui.busy {
		txt(hdc, "Processando...", R(w-172, h-113, 144, 25), teal, ui.miniFont, dtRight)
	}
}
func formatInt(n int) string {
	str := strconv.Itoa(n)
	for i := len(str) - 3; i > 0; i -= 3 {
		str = str[:i] + "." + str[i:]
	}
	return str
}
func stat(hdc uintptr, r rect, title, value, subtitle string) {
	round(hdc, r, panel, 13)
	txt(hdc, title, R(r.Left+17, r.Top+10, r.Right-r.Left-28, 23), muted, ui.miniFont, dtLeft)
	txt(hdc, value, R(r.Left+17, r.Top+31, r.Right-r.Left-28, 39), fg, ui.titleFont, dtLeft)
	txt(hdc, subtitle, R(r.Left+17, r.Top+75, r.Right-r.Left-28, 19), muted, ui.miniFont, dtLeft)
}
func placeSearch() {
	if ui == nil || ui.searchEdit == 0 {
		return
	}
	width := ui.width - 256 - 26 - 30
	if width < 100 {
		width = 100
	}
	pMoveWindow.Call(ui.searchEdit, 273, 270, uintptr(width), 31, 1)
}
func handleAction(action string) {
	if ui.busy {
		return
	}
	switch {
	case strings.HasPrefix(action, "inspect:"):
		id := strings.TrimPrefix(action, "inspect:")
		ui.busy = true
		writeMessage("Consultando dados públicos da Steam para App ID " + id + "...")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			result, err := steamStoreMetadata(ctx, &http.Client{Timeout: 12 * time.Second}, steamStoreAPI, id)
			inspectionMu.Lock()
			inspectionResult = result
			inspectionError = err
			inspectionMu.Unlock()
			pPostMessage.Call(ui.hwnd, wmApp+5, 0, 0)
		}()
	case strings.HasPrefix(action, "filter:"):
		ui.filter = strings.TrimPrefix(action, "filter:")
		ui.offset = 0
		updateResults()
	case strings.HasPrefix(action, "toggle:"):
		id, _ := strconv.ParseUint(strings.TrimPrefix(action, "toggle:"), 10, 64)
		if index, ok := ui.app.store.ByID[id]; ok {
			if err := ui.app.selection.Toggle(ui.app.store.Entries[index]); err != nil {
				writeMessage("Erro ao salvar seleção: " + err.Error())
			} else {
				writeMessage("Coleção atualizada. Nenhum arquivo Steam foi modificado.")
				updateResults()
			}
		}
	case action == "all":
		if ask("Adicionar tudo à coleção?", fmt.Sprintf("Adicionar os %s jogos e DLCs do catálogo à coleção LOCAL do Astronyx?\n\nIsso NÃO instala, ativa ou desbloqueia jogos na Steam.", formatInt(ui.app.store.TotalGameDLC))) {
			if err := ui.app.selection.AddAllGamesDLC(); err != nil {
				notify("Erro", err.Error())
			} else {
				ui.offset = 0
				updateResults()
				writeMessage(fmt.Sprintf("%s jogos e DLCs adicionados à coleção local!", formatInt(ui.app.store.TotalGameDLC)))
			}
		}
	case action == "catalog":
		filename := chooseFile("Selecionar catálogo GameList ZIP", "ZIP do catálogo (*.zip)\x00*.zip\x00Todos os arquivos (*.*)\x00*.*\x00")
		if filename == "" {
			return
		}
		ui.busy = true
		writeMessage("Importando e validando catálogo...")
		go func() {
			cat, err := importStoreCatalog(ui.app.dataDir, filename)
			catalogMu.Lock()
			catalogCandidate = cat
			catalogErr = err
			catalogMu.Unlock()
			pPostMessage.Call(ui.hwnd, wmApp+2, 0, 0)
		}()
	case action == "manifests":
		filename := chooseFile("Selecionar ZIP ou manifest", "Arquivos suportados\x00*.zip;*.acf;*.lua;*.manifest\x00Todos os arquivos\x00*.*\x00")
		if filename == "" {
			return
		}
		ui.busy = true
		writeMessage("Importando manifests com verificação de duplicados...")
		go func() {
			res := ImportResult{}
			var err error
			info, e := os.Stat(filename)
			if e != nil {
				err = e
			} else if info.Size() > 128<<20 {
				err = fmt.Errorf("Arquivo acima de 128 MB")
			} else {
				data, e := os.ReadFile(filename)
				if e != nil {
					err = e
				} else if strings.EqualFold(filepath.Ext(filename), ".zip") {
					res, err = ui.app.manifests.importZip(filepath.Base(filename), data)
				} else {
					added, e := ui.app.manifests.addFile(filepath.Base(filename), "Arquivo local", data)
					err = e
					if added {
						res.Added = 1
					} else if err == nil {
						res.Duplicates = 1
					}
				}
			}
			finishImport("Manifests", res, err)
		}()
	case action == "steam":
		ui.busy = true
		writeMessage("Procurando e importando manifests da instalação Steam...")
		go func() {
			_, override := ui.app.manifests.snapshot()
			scan := scanSteam(override)
			res := ImportResult{}
			if scan.Root == "" {
				finishImport("Steam local", res, fmt.Errorf("Cliente Steam não encontrado neste PC"))
				return
			}
			for i, g := range scan.Games {
				if i >= 4000 {
					break
				}
				src := filepath.Join(g.Library, "steamapps", "appmanifest_"+g.AppID+".acf")
				b, err := os.ReadFile(src)
				if err != nil {
					res.Skipped++
					continue
				}
				added, err := ui.app.manifests.addFile(filepath.Base(src), "Steam local", b)
				if err != nil {
					res.Skipped++
				} else if added {
					res.Added++
				} else {
					res.Duplicates++
				}
			}
			finishImport("Steam local", res, nil)
		}()
	case action == "folder":
		pShellExecute.Call(ui.hwnd, uintptr(unsafe.Pointer(wptr("open"))), uintptr(unsafe.Pointer(wptr(ui.app.dataDir))), 0, 0, 1)
	case action == "update":
		if ui.update.Available {
			if ask("Atualização disponível", "Baixar atualização assinada v"+ui.update.Latest+" e reiniciar o aplicativo?") {
				ui.busy = true
				writeMessage("Baixando atualização validada...")
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
					defer cancel()
					status := ui.app.updater.check(ctx, true)
					e := ""
					if !status.Available {
						e = "Nenhuma nova versão disponível."
					} else {
						ui.app.updater.mu.Lock()
						release := ui.app.updater.cached
						ui.app.updater.mu.Unlock()
						staged, err := downloadUpdate(ctx, release)
						if err != nil {
							e = err.Error()
						} else if err = launchInstaller(staged, release.SHA256); err != nil {
							e = err.Error()
						}
					}
					updateMu.Lock()
					updateErr = e
					updateMu.Unlock()
					pPostMessage.Call(ui.hwnd, wmApp+3, 0, 0)
				}()
			}
		} else {
			notify("Atualizações", ui.update.Error+"\nConfigure UPDATE_FEED_URL e UPDATE_PUBLIC_KEY na publicação do aplicativo.")
		}
	}
}
func chooseFile(title, filter string) string {
	buf := make([]uint16, 4096)
	// Win32 filter uses embedded NULs and ends with an extra NUL.
	uFilter := append(utf16.Encode([]rune(filter)), 0)
	of := openFileName{Size: uint32(unsafe.Sizeof(openFileName{})), HwndOwner: ui.hwnd, Filter: &uFilter[0], File: &buf[0], MaxFile: uint32(len(buf)), Title: wptr(title), Flags: ofnFileMustExist | ofnPathMustExist | ofnExplorer}
	ret, _, _ := pOpenFile.Call(uintptr(unsafe.Pointer(&of)))
	if ret == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

var inspectionMu sync.Mutex
var inspectionResult StoreInspection
var inspectionError error
var importMu sync.Mutex
var lastImportResult ImportResult
var lastImportError error
var lastImportSource string

func finishImport(source string, result ImportResult, err error) {
	importMu.Lock()
	lastImportResult = result
	lastImportError = err
	lastImportSource = source
	importMu.Unlock()
	pPostMessage.Call(ui.hwnd, wmApp+4, 0, 0)
}

var updateMu sync.Mutex
var receivedUpdate UpdateStatus
var updateErr string
var catalogMu sync.Mutex
var catalogCandidate *StoreCatalog
var catalogErr error

func refreshUpdater() {
	if ui == nil || ui.app == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	status := ui.app.updater.check(ctx, false)
	updateMu.Lock()
	receivedUpdate = status
	updateMu.Unlock()
	pPostMessage.Call(ui.hwnd, wmApp+1, 0, 0)
}
