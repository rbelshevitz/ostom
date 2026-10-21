---
name: ostom-dotnet-gui
description: Maintain and improve the OSTOM .NET WinForms GUI as a compact Windows recovery utility without changing the recovery CLI contract.
---

# OSTOM .NET GUI

Use this skill for work under `gui/Ostom.Gui`, including WinForms layout,
state feedback, packaging, and the relationship between the GUI and the
adjacent recovery CLI.

## Product boundary

- The GUI is a thin WinForms front end for `ostom.exe`; preserve the CLI
  arguments, JSON report shape, progress JSON, cancellation, and bilingual
  behavior unless the task explicitly changes the contract.
- The GUI must find `ostom.exe` beside `ostom-gui.exe`. Release packaging must
  place both files in the same output directory.
- The source application icon is `gui/Ostom.Gui/ostom.ico`; keep the project
  `ApplicationIcon` setting working. The runtime form extracts the associated
  icon from its own executable, so the ICO does not need to ship beside the
  published GUI.
- Do not move recovery or export logic into the GUI.

## Compact utility language

- Prefer a short, task-first window over a branded hero layout. Do not add
  decorative title blocks, explanatory paragraphs, gradients, or dashboard
  chrome unless they directly help file selection, analysis, export, or
  recovery interpretation.
- Keep the order obvious: source file, export folder, optional EML choice,
  Analyze, Export, Cancel, result, status.
- File-picker and action buttons use equal widths and aligned baselines.
  Export is the primary action; Analyze is secondary; Cancel is only enabled
  during work.
- Keep the report area larger than the input chrome. Prefer concise status
  summaries or cards; put verbose technical details behind disclosure when
  the UI supports it.
- The current compact layout uses `Источник и экспорт` / `Source and export`,
  `Результат` / `Result`, a bottom status strip, and a separate processed-count
  line above the progress bar. Preserve this hierarchy when adjusting layout.

## State and feedback

- On first launch the status must say `Ожидаем файл` / `Waiting for file`, not
  `Готово` / `Ready`; no work has happened yet.
- Use explicit text for waiting, working, complete, warning, partial,
  cancelled, and failed states. Color is secondary and must not carry meaning
  alone.
- Determinate progress shows processed/total. Indeterminate progress shows
  what operation is active. Keep the Cancel action visible while busy.
- Preserve report content after completion and preserve useful failure text
  after cancellation or a missing CLI.
- Status text must be meaningful without color: waiting for a file, working,
  completed, completed with limitations/warnings, cancelled, and failed.

## Layout and accessibility

- Keep `AutoScaleMode = Dpi` and test at 100%, 125%, 150%, and 200% scaling.
- Check long Russian and English strings, keyboard focus, tab order, disabled
  states, and high-contrast-friendly contrast.
- Avoid fixed widths that make localized labels clip. If commands do not fit,
  reduce decoration or use a deliberate overflow/grouping strategy.
- The current window is intentionally non-maximizable with a resize grip;
  retain usable resizing and test the fixed action-button widths with both
  Russian and English labels.

## Verification

From the repository root, publish the GUI as a self-contained single-file
`win-x64` application, then build the adjacent recovery CLI:

```powershell
dotnet publish .\gui\Ostom.Gui\Ostom.Gui.csproj -c Release -r win-x64 --self-contained true -v:q
go build -trimpath -o .\gui\Ostom.Gui\bin\Release\net8.0-windows\win-x64\publish\ostom.exe .\cmd\ostom
```

Then verify that the publish folder contains `ostom-gui.exe` and `ostom.exe`,
while the GUI's .NET DLLs, runtime DLLs, and JSON support
files are embedded in the single executable rather than shipped beside it.
The Go CLI remains a separate executable by design. Do not claim the GUI is
ready without checking the missing-CLI and cancelled-operation paths when
those code paths are changed. If the icon or project packaging changes, also
verify the window/taskbar icon in a packaged build rather than only in the
source tree; the source `ostom.ico` should not be required beside the
published EXE.
