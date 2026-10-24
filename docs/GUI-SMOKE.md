# OSTOM GUI smoke checklist

Run this checklist against the published GUI with `ostom-gui.exe` and the
matching `ostom.exe` in the same directory. Repeat the language-sensitive
checks under a Russian (`ru-*`) and an English (`en-*`) Windows user locale.

## Launch and layout

- [ ] The window opens without requiring `ostom.ico` beside the published EXE.
- [ ] Startup status says `Ожидаем файл` / `Waiting for file`.
- [ ] Source and export controls are aligned; Analyze, Export, and Cancel have
      usable keyboard focus and tab order.
- [ ] At 100%, 125%, 150%, and 200% display scaling, no primary control or
      status text is clipped.

## Input and process states

- [ ] Analyze/Export with an empty source is rejected with a localized message.
- [ ] With `ostom.exe` absent, the GUI reports a localized CLI-not-found error,
      preserves the useful report text, and returns to an idle state.
- [ ] Cancel during analysis and export stops the child process, reports a
      localized cancelled state, and re-enables the controls.
- [ ] A successful export shows processed count, ETA/rate when available, and
      preserves the final report.
- [ ] A partial result shows a warning/partial state in text, not only color.
- [ ] A failed export shows failure details and leaves the GUI usable.

## Progress and language

- [ ] Russian locale uses Russian labels, including `сообщений/с`,
      `кандидатов` only in technical details, and localized ETA units.
- [ ] English locale uses English labels, including `messages/s` and ETA units.
- [ ] Unknown totals show throughput without inventing an ETA.
- [ ] Known totals show `done/total` and a bounded approximate ETA.
- [ ] Progress JSON and final report remain valid JSON/JSONL in both locales.

## Evidence to record

Record Windows version, .NET Desktop Runtime version, display scale, locale,
source fixture, export result, and any clipped or misleading text. For the
long-run acceptance check use `testdata/fixtures/fifty.ost` and expect 2,178
messages, 168 attachments, zero partial messages, and zero attachment failures.
