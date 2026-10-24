//go:build windows

// ostom-gui is a small Windows shell around the cross-platform ostom CLI.
// Keeping the recovery engine in a child process makes the UI responsive even
// when a damaged file needs the native watchdog path.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmCommand        = 0x0111
	wmClose          = 0x0010
	wmDestroy        = 0x0002
	wmCtlColorStatic = 0x0138
	wmApp            = 0x8000
	wmProgress       = wmApp + 2
	wmRunDone        = wmApp + 1
	wmOutputDone     = wmApp + 3
	wmSourceDone     = wmApp + 4

	idSource       = 1001
	idOutput       = 1002
	idBrowse       = 1003
	idRun          = 1004
	idEML          = 1005
	idStatus       = 1006
	idCancel       = 1007
	idProgress     = 1008
	idOutputBrowse = 1009
)

type hwnd = windows.Handle

type guiState struct {
	mu                  sync.Mutex
	hwnd                hwnd
	source              hwnd
	output              hwnd
	status              hwnd
	progress            hwnd
	eml                 hwnd
	browse              hwnd
	outputBrowse        hwnd
	run                 hwnd
	cancel              hwnd
	running             bool
	cancelled           bool
	process             *os.Process
	lastText            string
	liveText            string
	selectedOutput      string
	outputDialogRunning bool
	selectedSource      string
	sourceDialogError   bool
	sourceDialogRunning bool
}

type runResult struct {
	text string
	err  error
}

type reportView struct {
	Status    string `json:"status"`
	Source    string `json:"source"`
	SourceB   int64  `json:"source_bytes"`
	CheckOnly bool   `json:"check_only"`
	Findings  struct {
		Structure   string `json:"structure"`
		Messages    string `json:"messages"`
		Attachments string `json:"attachments"`
	} `json:"findings"`
	Assessment struct {
		RecoveryOutlook string `json:"recovery_outlook"`
		DeepRecovery    string `json:"deep_recovery"`
		Integrity       string `json:"integrity"`
		Summary         string `json:"summary"`
		SummaryEnglish  string `json:"summary_en"`
	} `json:"assessment"`
	Indexed struct {
		ExportedMessages int    `json:"exported_messages"`
		FailedMessages   int    `json:"failed_messages"`
		Messages         int    `json:"messages"`
		Attachments      int    `json:"attachments"`
		MessagesPartial  int    `json:"messages_partial"`
		EMLMode          string `json:"eml_mode"`
	} `json:"indexed_export"`
	Native struct {
		Status           string `json:"status"`
		RecoveredRecords int    `json:"recovered_records"`
		EMLMode          string `json:"eml_mode"`
		Export           struct {
			Written               int `json:"written"`
			Attachments           int `json:"attachments"`
			Incomplete            int `json:"incomplete"`
			IncompleteAttachments int `json:"incomplete_attachments"`
		} `json:"export"`
		Memory struct {
			Peak struct {
				RuntimeBytes    uint64 `json:"runtime_bytes"`
				WorkingSetBytes uint64 `json:"working_set_bytes"`
			} `json:"peak"`
		} `json:"memory"`
	} `json:"native_recovery"`
	Storage *struct {
		AvailableBytes   uint64 `json:"available_bytes"`
		RequiredEstimate uint64 `json:"required_estimate_bytes"`
		Warning          string `json:"warning"`
	} `json:"storage"`
	Warnings []string `json:"warnings"`
	Errors   []string `json:"errors"`
}

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	comdlg32             = windows.NewLazySystemDLL("comdlg32.dll")
	comctl32             = windows.NewLazySystemDLL("comctl32.dll")
	shell32              = windows.NewLazySystemDLL("shell32.dll")
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	gdi32                = windows.NewLazySystemDLL("gdi32.dll")
	procDefWindow        = user32.NewProc("DefWindowProcW")
	procShow             = user32.NewProc("ShowWindow")
	procUpdate           = user32.NewProc("UpdateWindow")
	procRegister         = user32.NewProc("RegisterClassExW")
	procCreate           = user32.NewProc("CreateWindowExW")
	procGetMsg           = user32.NewProc("GetMessageW")
	procTranslate        = user32.NewProc("TranslateMessage")
	procDispatch         = user32.NewProc("DispatchMessageW")
	procPost             = user32.NewProc("PostMessageW")
	procSend             = user32.NewProc("SendMessageW")
	procDestroy          = user32.NewProc("DestroyWindow")
	procSetText          = user32.NewProc("SetWindowTextW")
	procGetSysColor      = user32.NewProc("GetSysColor")
	procGetSysColorBrush = user32.NewProc("GetSysColorBrush")
	procGetText          = user32.NewProc("GetWindowTextW")
	procGetLen           = user32.NewProc("GetWindowTextLengthW")
	procEnable           = user32.NewProc("EnableWindow")
	procMessage          = user32.NewProc("MessageBoxW")
	procGetMod           = kernel32.NewProc("GetModuleHandleW")
	procGetUILang        = kernel32.NewProc("GetUserDefaultUILanguage")
	procOpenFile         = comdlg32.NewProc("GetOpenFileNameW")
	procInitCC           = comctl32.NewProc("InitCommonControlsEx")
	procGetStock         = gdi32.NewProc("GetStockObject")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procBrowseFolder     = shell32.NewProc("SHBrowseForFolderW")
	procGetFolderPath    = shell32.NewProc("SHGetPathFromIDListW")
	procFreePIDL         = ole32.NewProc("CoTaskMemFree")
	procCoInitialize     = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
)

type point struct{ x, y int32 }
type initCommonControlsEx struct {
	size    uint32
	classes uint32
}
type browseInfo struct {
	hwndOwner   hwnd
	pidlRoot    *byte
	displayName *uint16
	title       *uint16
	flags       uint32
	callback    uintptr
	lParam      uintptr
	iImage      int32
}
type msg struct {
	hwnd    hwnd
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}
type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     hwnd
	hIcon         hwnd
	hCursor       hwnd
	hbrBackground hwnd
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       hwnd
}
type openFileName struct {
	lStruct        uint32
	hwndOwner      hwnd
	hInstance      hwnd
	filter         *uint16
	customFilter   *uint16
	nMaxCustFilter uint32
	nFilterIndex   uint32
	file           *uint16
	nMaxFile       uint32
	fileTitle      *uint16
	nMaxFileTitle  uint32
	initialDir     *uint16
	title          *uint16
	flags          uint32
	fileOffset     uint16
	fileExtension  uint16
	defExt         *uint16
	customData     uintptr
	hook           uintptr
	templateName   *uint16
	reserved       *uint16
	reserved2      uint32
	flagsEx        uint32
}

var state guiState
var englishUI bool

func detectEnglishUI() bool {
	lang, _, _ := procGetUILang.Call()
	// LANGID primary language 0x19 is Russian; unknown locales use English.
	return uint16(lang)&0x03ff != 0x19
}

func tr(russian, english string) string {
	if englishUI {
		return english
	}
	return russian
}

func main() {
	englishUI = detectEnglishUI()
	cc := initCommonControlsEx{size: uint32(unsafe.Sizeof(initCommonControlsEx{})), classes: 0x20}
	procInitCC.Call(uintptr(unsafe.Pointer(&cc))) // ICC_PROGRESS_CLASS
	instance, _, _ := procGetMod.Call(0)
	className, _ := windows.UTF16PtrFromString("OstomRecoveryWindow")
	procRegister.Call(uintptr(unsafe.Pointer(&wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   windows.NewCallback(windowProc),
		hInstance:     hwnd(instance),
		hbrBackground: hwnd(16), // COLOR_BTNFACE + 1
		lpszClassName: className,
	})))
	h, _, _ := procCreate.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(mustUTF16(tr("OSTOM — восстановление PST/OST", "OSTOM — PST/OST recovery")))), 0x00CF0000, 100, 100, 760, 470, 0, 0, instance, 0)
	if h == 0 {
		return
	}
	state.hwnd = hwnd(h)
	procShow.Call(h, 1)
	procUpdate.Call(h)
	var m msg
	for {
		ret, _, _ := procGetMsg.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslate.Call(uintptr(unsafe.Pointer(&m)))
		procDispatch.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func windowProc(h hwnd, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case 1: // WM_CREATE
		createControls(h)
	case wmCommand:
		id := int(wParam & 0xffff)
		if id == idBrowse {
			chooseSource(h)
		}
		if id == idOutputBrowse {
			chooseOutput(h)
		}
		if id == idRun {
			startRun(h)
		}
		if id == idCancel {
			cancelRun()
		}
	case wmRunDone:
		finishRun(h)
	case wmOutputDone:
		state.mu.Lock()
		path := state.selectedOutput
		state.selectedOutput = ""
		state.outputDialogRunning = false
		state.mu.Unlock()
		setEnabled(state.outputBrowse, true)
		if path != "" {
			setText(state.output, path)
			setText(state.status, tr("Папка экспорта выбрана.", "Export folder selected."))
		} else {
			setText(state.status, tr("Выбор папки отменён.", "Folder selection cancelled."))
		}
	case wmSourceDone:
		state.mu.Lock()
		path := state.selectedSource
		errorText := state.sourceDialogError
		state.selectedSource = ""
		state.sourceDialogError = false
		state.sourceDialogRunning = false
		state.mu.Unlock()
		setEnabled(state.browse, true)
		if path != "" {
			setText(state.source, path)
			setText(state.output, path+".ostom-export")
			setText(state.status, tr("Файл PST/OST выбран.", "PST/OST file selected."))
		} else if errorText {
			setText(state.status, tr("Можно выбрать только файл .pst или .ost.", "Only .pst or .ost files can be selected."))
		} else {
			setText(state.status, tr("Выбор файла отменён.", "File selection cancelled."))
		}
	case wmClose:
		procDestroy.Call(uintptr(h))
	case wmDestroy:
		postQuit := user32.NewProc("PostQuitMessage")
		postQuit.Call(0)
	case wmProgress:
		state.mu.Lock()
		text := state.liveText
		state.mu.Unlock()
		setText(state.status, text)
	case wmCtlColorStatic:
		procSetBkMode.Call(wParam, 1)           // TRANSPARENT
		color, _, _ := procGetSysColor.Call(18) // COLOR_BTNTEXT
		procSetTextColor.Call(wParam, color, 0)
		brush, _, _ := procGetSysColorBrush.Call(15) // COLOR_BTNFACE
		return brush
	default:
		ret, _, _ := procDefWindow.Call(uintptr(h), uintptr(message), wParam, lParam)
		return ret
	}
	return 0
}

func createControls(parent hwnd) {
	state.source = control("EDIT", "", 0x50810000, 18, 24, 570, 28, idSource, parent)
	state.browse = control("BUTTON", tr("Открыть…", "Open…"), 0x50010000, 600, 24, 120, 28, idBrowse, parent)
	state.output = control("EDIT", "", 0x50810000, 18, 70, 570, 28, idOutput, parent)
	state.outputBrowse = control("BUTTON", tr("Папка…", "Folder…"), 0x50010000, 600, 70, 120, 28, idOutputBrowse, parent)
	control("STATIC", tr("Источник PST/OST", "PST/OST source"), 0x50000000, 18, 4, 180, 16, 0, parent)
	control("STATIC", tr("Папка экспорта mbox", "mbox export folder"), 0x50000000, 18, 50, 220, 16, 0, parent)
	state.eml = control("BUTTON", tr("Дополнительно сохранить EML", "Also save individual EML files"), 0x50010003, 18, 104, 260, 26, idEML, parent)
	state.run = control("BUTTON", tr("Проверить и экспортировать", "Check and export"), 0x50010000, 300, 102, 250, 30, idRun, parent)
	state.cancel = control("BUTTON", tr("Отмена", "Cancel"), 0x50000000, 560, 102, 160, 30, idCancel, parent)
	setEnabled(state.cancel, false)
	state.progress = control("msctls_progress32", "", 0x50000008, 18, 140, 702, 10, idProgress, parent)
	state.status = control("EDIT", tr("Готов. Выберите PST/OST.", "Ready. Select a PST/OST file."), 0x50B00004, 18, 158, 702, 260, idStatus, parent)
	setMarquee(false)
}

func control(class, text string, style uint32, x, y, w, h, id int, parent hwnd) hwnd {
	instance, _, _ := procGetMod.Call(0)
	cn, _ := windows.UTF16PtrFromString(class)
	t, _ := windows.UTF16PtrFromString(text)
	result, _, _ := procCreate.Call(0, uintptr(unsafe.Pointer(cn)), uintptr(unsafe.Pointer(t)), uintptr(style), uintptr(x), uintptr(y), uintptr(w), uintptr(h), uintptr(parent), uintptr(id), instance, 0)
	procSend.Call(result, 0x0030, fontResult(procGetStock.Call(17)), 1) // WM_SETFONT / DEFAULT_GUI_FONT
	return hwnd(result)
}

func fontResult(value uintptr, _ uintptr, _ error) uintptr { return value }

func mustUTF16(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }

// Windows file-dialog filters are MULTI_SZ values. UTF16PtrFromString rejects
// embedded NULs, so construct the value explicitly.
func mustUTF16Multi(parts ...string) *uint16 {
	values := make([]uint16, 0, 64)
	for _, part := range parts {
		encoded, _ := windows.UTF16FromString(part)
		values = append(values, encoded[:len(encoded)-1]...)
		values = append(values, 0)
	}
	values = append(values, 0)
	return &values[0]
}

func chooseSource(parent hwnd) {
	state.mu.Lock()
	if state.sourceDialogRunning {
		state.mu.Unlock()
		return
	}
	state.sourceDialogRunning = true
	state.mu.Unlock()
	setEnabled(state.browse, false)
	setText(state.status, tr("Открываю выбор PST/OST…", "Opening PST/OST picker…"))
	go func() {
		procCoInitialize.Call(0, 0x2) // COM STA for this common-dialog thread.
		defer procCoUninitialize.Call()
		filter := mustUTF16Multi("PST files (*.pst)", "*.pst", "OST files (*.ost)", "*.ost")
		name := mustUTF16(tr("Выберите PST или OST", "Select a PST or OST file"))
		var selected string
		invalid := false
		for {
			buffer := make([]uint16, 32768)
			of := openFileName{lStruct: uint32(unsafe.Sizeof(openFileName{})), hwndOwner: parent, filter: filter, file: &buffer[0], nMaxFile: uint32(len(buffer)), title: name, flags: 0x00001000 | 0x00000800}
			ret, _, _ := procOpenFile.Call(uintptr(unsafe.Pointer(&of)))
			if ret == 0 {
				break
			}
			candidate := windows.UTF16ToString(buffer[:])
			ext := strings.ToLower(filepath.Ext(candidate))
			if ext == ".pst" || ext == ".ost" {
				selected = candidate
				break
			}
			invalid = true
		}
		state.mu.Lock()
		state.selectedSource = selected
		state.sourceDialogError = invalid && selected == ""
		state.mu.Unlock()
		procPost.Call(uintptr(parent), wmSourceDone, 0, 0)
	}()
}

func chooseOutput(parent hwnd) {
	state.mu.Lock()
	if state.outputDialogRunning {
		state.mu.Unlock()
		return
	}
	state.outputDialogRunning = true
	state.mu.Unlock()
	setEnabled(state.outputBrowse, false)
	setText(state.status, tr("Открываю выбор папки…", "Opening folder picker…"))
	go func() {
		procCoInitialize.Call(0, 0x2) // COM STA for this shell-dialog thread.
		defer procCoUninitialize.Call()
		display := make([]uint16, 260)
		info := browseInfo{
			hwndOwner:   parent,
			displayName: &display[0],
			title:       mustUTF16(tr("Выберите папку для экспорта mbox", "Select the mbox export folder")),
			flags:       0x0001 | 0x0010 | 0x0040, // FSDIRS | EDITBOX | NEWDIALOGSTYLE
		}
		var selected string
		pidl, _, _ := procBrowseFolder.Call(uintptr(unsafe.Pointer(&info)))
		if pidl != 0 {
			defer procFreePIDL.Call(pidl)
			path := make([]uint16, 32768)
			if ret, _, _ := procGetFolderPath.Call(pidl, uintptr(unsafe.Pointer(&path[0]))); ret != 0 {
				selected = windows.UTF16ToString(path[:])
			}
		}
		state.mu.Lock()
		state.selectedOutput = selected
		state.mu.Unlock()
		procPost.Call(uintptr(parent), wmOutputDone, 0, 0)
	}()
}

func startRun(parent hwnd) {
	state.mu.Lock()
	if state.running {
		state.mu.Unlock()
		return
	}
	state.running = true
	state.cancelled = false
	state.mu.Unlock()
	source := getText(state.source)
	output := getText(state.output)
	if source == "" || output == "" {
		setText(state.status, tr("Укажите исходный файл и папку экспорта.", "Select a source file and an export folder."))
		state.mu.Lock()
		state.running = false
		state.mu.Unlock()
		return
	}
	setText(state.status, tr("Анализ запущен… большие файлы обрабатываются потоково; окно останется отзывчивым.", "Analysis started… large files are processed as a stream; the window will remain responsive."))
	setBusy(true)
	setMarquee(true)
	includeEML := isChecked(state.eml)
	go func() {
		exe, _ := os.Executable()
		cli := filepath.Join(filepath.Dir(exe), "ostom.exe")
		if _, err := os.Stat(cli); err != nil {
			if found, lookErr := exec.LookPath("ostom.exe"); lookErr == nil {
				cli = found
			} else {
				state.mu.Lock()
				state.lastText = ""
				state.liveText = tr("Не найден ostom.exe рядом с GUI или в PATH.", "ostom.exe was not found next to the GUI or in PATH.")
				state.mu.Unlock()
				procPost.Call(uintptr(parent), wmRunDone, 0, 0)
				return
			}
		}
		args := []string{"--json", "--progress-json", "--output-dir", output, "--export-dir", output, source}
		if includeEML {
			args = append(args[:0], append([]string{"--json", "--progress-json", "--eml", "--output-dir", output, "--export-dir", output}, source)...)
		}
		cmd := exec.Command(cli, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		var stdout strings.Builder
		cmd.Stdout = &stdout
		stderr, pipeErr := cmd.StderrPipe()
		if pipeErr == nil {
			if startErr := cmd.Start(); startErr == nil {
				state.mu.Lock()
				state.process = cmd.Process
				state.mu.Unlock()
				scanner := bufio.NewScanner(stderr)
				for scanner.Scan() {
					var progress struct {
						Label string `json:"label"`
						Done  int    `json:"done"`
						Total int    `json:"total"`
					}
					if json.Unmarshal(scanner.Bytes(), &progress) == nil && progress.Label != "" {
						setProgress(progress.Done, progress.Total)
						state.mu.Lock()
						label := localizedProgressLabel(progress.Label)
						if progress.Total > 0 {
							state.liveText = fmt.Sprintf("%s: %d/%d…", label, progress.Done, progress.Total)
						} else {
							state.liveText = label + "…"
						}
						state.mu.Unlock()
						procPost.Call(uintptr(parent), wmProgress, 0, 0)
					}
				}
				_ = cmd.Wait()
			} else {
				_, _ = io.Copy(io.Discard, stderr)
				_ = cmd.Run()
			}
		} else {
			_ = cmd.Run()
		}
		state.mu.Lock()
		state.process = nil
		state.lastText = stdout.String()
		state.mu.Unlock()
		procPost.Call(uintptr(parent), wmRunDone, 0, 0)
	}()
}

func cancelRun() {
	state.mu.Lock()
	if !state.running {
		state.mu.Unlock()
		return
	}
	state.cancelled = true
	process := state.process
	state.mu.Unlock()
	if process != nil {
		_ = process.Kill()
	}
	setText(state.status, tr("Отмена запрошена… закрываю дочерний процесс и сохраняю уже найденный результат.", "Cancellation requested… stopping the child process; already recovered output is kept."))
}

func setBusy(busy bool) {
	setEnabled(state.browse, !busy)
	setEnabled(state.outputBrowse, !busy)
	setEnabled(state.run, !busy)
	setEnabled(state.eml, !busy)
	setEnabled(state.source, !busy)
	setEnabled(state.output, !busy)
	setEnabled(state.cancel, busy)
}

func setMarquee(active bool) {
	if state.progress != 0 {
		procSend.Call(uintptr(state.progress), 0x040A, boolToUintptr(active), 35) // PBM_SETMARQUEE
	}
}

func setProgress(done, total int) {
	if state.progress == 0 {
		return
	}
	if total <= 0 {
		setMarquee(true)
		return
	}
	setMarquee(false)
	procSend.Call(uintptr(state.progress), 0x0401, 0, uintptr(total)) // PBM_SETRANGE32
	procSend.Call(uintptr(state.progress), 0x0402, uintptr(done), 0)  // PBM_SETPOS
}

func setEnabled(h hwnd, enabled bool) {
	if h != 0 {
		procEnable.Call(uintptr(h), boolToUintptr(enabled))
	}
}

func boolToUintptr(value bool) uintptr {
	if value {
		return 1
	}
	return 0
}

func finishRun(_ hwnd) {
	state.mu.Lock()
	state.running = false
	cancelled := state.cancelled
	text := state.lastText
	state.mu.Unlock()
	setBusy(false)
	setMarquee(false)
	if cancelled {
		setText(state.status, tr("Операция отменена пользователем. Полный итоговый отчёт не создавался; исходный файл не изменён.", "Operation cancelled. No complete final report was created; the source file was not modified."))
		return
	}
	var view reportView
	if err := json.Unmarshal([]byte(text), &view); err != nil {
		setText(state.status, tr("Не удалось прочитать итоговый отчёт CLI: ", "Could not read the CLI final report: ")+err.Error())
		return
	}
	var b strings.Builder
	assessmentSummary := view.Assessment.Summary
	if englishUI && view.Assessment.SummaryEnglish != "" {
		assessmentSummary = view.Assessment.SummaryEnglish
	}
	fmt.Fprintf(&b, "%s: %s\r\n%s: %s\r\n%s: %s\r\n%s: %s\r\n%s: %s\r\n", tr("Статус", "Status"), humanStatus(view.Status), tr("Состояние файла", "File condition"), assessmentSummary, tr("Сообщения", "Messages"), humanFinding(view.Findings.Messages, "messages"), tr("Вложения", "Attachments"), humanFinding(view.Findings.Attachments, "attachments"), tr("Результат восстановления", "Recovery result"), humanOutlook(view.Assessment.RecoveryOutlook))
	if view.Indexed.Messages > 0 || view.Native.Export.Written > 0 {
		fmt.Fprintf(&b, "%s: %d %s, %d %s; %s: %d %s, %d %s\r\n", tr("Обычный экспорт", "Regular export"), view.Indexed.Messages, tr("сообщений", "messages"), view.Indexed.Attachments, tr("вложений", "attachments"), tr("Спасательное восстановление", "Salvage recovery"), view.Native.Export.Written, tr("сообщений", "messages"), view.Native.Export.Attachments, tr("вложений", "attachments"))
	}
	if view.Indexed.MessagesPartial > 0 || view.Native.Export.Incomplete > 0 || view.Native.Export.IncompleteAttachments > 0 {
		fmt.Fprintf(&b, "%s: %d %s, %d %s\r\n", tr("Частично записано", "Partial output"), view.Indexed.MessagesPartial+view.Native.Export.Incomplete, tr("сообщений", "messages"), view.Native.Export.IncompleteAttachments, tr("вложений", "attachments"))
	}
	fmt.Fprintf(&b, "%s: %s\r\n", tr("Результат", "Output"), getText(state.output))
	if view.Indexed.EMLMode != "" || view.Native.EMLMode != "" {
		mode := view.Indexed.EMLMode
		if mode == "" {
			mode = view.Native.EMLMode
		}
		fmt.Fprintf(&b, "%s: %s\r\n", tr("Режим файлов", "File mode"), humanFileMode(mode))
	}
	if view.Storage != nil {
		fmt.Fprintf(&b, "%s: %s; %s: %s\r\n", tr("Свободно на диске", "Free disk space"), humanBytes(view.Storage.AvailableBytes), tr("оценка экспорта", "export estimate"), humanBytes(view.Storage.RequiredEstimate))
		if view.Storage.Warning != "" {
			fmt.Fprintf(&b, "%s: %s\r\n", tr("Диск", "Disk"), tr("свободного места может не хватить для результата", "available space may be insufficient for the output"))
		}
	}
	if view.Native.Memory.Peak.RuntimeBytes > 0 {
		fmt.Fprintf(&b, "%s: %s; %s: %s\r\n", tr("Пиковая память Go", "Peak Go runtime memory"), humanBytes(view.Native.Memory.Peak.RuntimeBytes), tr("рабочий набор процесса", "process working set"), humanBytes(view.Native.Memory.Peak.WorkingSetBytes))
	}
	if len(view.Warnings) > 0 {
		fmt.Fprintf(&b, "%s: %s\r\n", tr("Предупреждения", "Warnings"), humanIssueCount(len(view.Warnings), false))
	}
	if len(view.Errors) > 0 {
		fmt.Fprintf(&b, "%s: %s\r\n", tr("Ошибки", "Errors"), humanIssueCount(len(view.Errors), true))
	}
	fmt.Fprintf(&b, "%s: %s\r\n", tr("Подробный отчёт", "Detailed report"), filepath.Join(getText(state.output), "ostom-report.json"))
	setText(state.status, b.String())
}

func getText(h hwnd) string {
	n, _, _ := procGetLen.Call(uintptr(h))
	buf := make([]uint16, n+1)
	procGetText.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), n+1)
	return windows.UTF16ToString(buf)
}
func setText(h hwnd, value string) {
	p, _ := windows.UTF16PtrFromString(value)
	procSetText.Call(uintptr(h), uintptr(unsafe.Pointer(p)))
}
func isChecked(h hwnd) bool { ret, _, _ := procSend.Call(uintptr(h), 0x00F0, 0, 0); return ret == 1 }

func humanBytes(value uint64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	amount := float64(value)
	unit := 0
	for amount >= 1024 && unit < len(units)-1 {
		amount /= 1024
		unit++
	}
	return fmt.Sprintf("%.1f %s", amount, units[unit])
}

func humanFinding(value, kind string) string {
	switch value {
	case "complete":
		if englishUI {
			return "readable results found"
		}
		return "результат прочитан полностью"
	case "partial":
		if englishUI {
			return "some results are partial"
		}
		return "часть результата восстановлена"
	case "failed":
		if englishUI {
			return "could not be read"
		}
		return "прочитать не удалось"
	case "not_checked":
		if englishUI {
			return "not checked"
		}
		return "не проверялись"
	default:
		if kind == "attachments" {
			return tr("состояние вложений не определено", "attachment status is unknown")
		}
		return tr("состояние сообщений не определено", "message status is unknown")
	}
}

func humanStatus(value string) string {
	switch value {
	case "completed":
		return tr("Готово", "Completed")
	case "completed with errors":
		return tr("Готово, но с ошибками", "Completed with errors")
	case "failed":
		return tr("Не удалось завершить", "Could not complete")
	default:
		return tr("Результат получен", "Result available")
	}
}

func humanIssueCount(count int, errors bool) string {
	if englishUI {
		if errors {
			return fmt.Sprintf("%d error(s); see the detailed report", count)
		}
		return fmt.Sprintf("%d warning(s); see the detailed report", count)
	}
	if errors {
		return fmt.Sprintf("%d; подробности сохранены в отчёте", count)
	}
	return fmt.Sprintf("%d; подробности сохранены в отчёте", count)
}

func humanFileMode(value string) string {
	switch value {
	case "all":
		return tr("сообщения и отдельные файлы EML", "messages and individual EML files")
	case "none":
		return tr("только дерево почты", "mailbox tree only")
	default:
		return tr("обычный режим", "standard mode")
	}
}

func humanOutlook(value string) string {
	switch value {
	case "good":
		return tr("обычного чтения должно хватить", "ordinary reading should be sufficient")
	case "uncertain":
		return tr("результат не гарантирован; проверяем доступные данные", "the result is uncertain; available data will be checked")
	case "limited":
		return tr("доступна только часть данных", "only part of the data is expected to be available")
	default:
		return tr("не определён", "unknown")
	}
}

func localizedProgressLabel(label string) string {
	switch label {
	case "scanning recoverable messages":
		return tr("Поиск восстанавливаемых сообщений", "Scanning recoverable messages")
	case "writing recovered messages":
		return tr("Запись восстановленных сообщений", "Writing recovered messages")
	case "standard mailbox export":
		return tr("Обычный экспорт почты", "Standard mailbox export")
	case "preparing recovery":
		return tr("Подготовка восстановления", "Preparing recovery")
	default:
		return tr("Идёт обработка", "Processing")
	}
}
