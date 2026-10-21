---
name: ostom-icon
description: Create, review, or package the OSTOM application icon used by the .NET WinForms GUI.
---

# OSTOM application icon

Use this skill for the icon assets of `gui/Ostom.Gui`, especially
`ostom-icon-generated.png` and `ostom.ico`. It covers visual revisions,
conversion, and wiring the icon into the Windows GUI. It does not replace the
GUI skill for layout or behavior changes.

## Asset contract

- `ostom-icon-generated.png` is the editable/source visual when a raster
  revision is needed.
- `ostom.ico` is the shipping Windows icon. Keep it in
  `gui/Ostom.Gui/`, referenced by `<ApplicationIcon>ostom.ico</ApplicationIcon>`
  in the project, and copied to Release/publish output.
- Preserve transparency, a clean silhouette at small sizes, and sufficient
  contrast on light and dark Windows surfaces. Avoid tiny text or details that
  disappear at 16–32 px.
- Do not silently replace the icon with a generic framework icon or add a
  second competing icon asset without a packaging reason.

## Change workflow

When the artwork changes, inspect the source PNG, make the smallest visual
revision that answers the request, regenerate `ostom.ico` with multiple
Windows-appropriate sizes if the conversion tool supports them, and verify
that the ICO is actually used both by the project metadata and by
`MainForm.Icon` at runtime.

For a new visual concept, keep the OSTOM identity recognizable: a restrained
recovery/repair-tool mark, strong silhouette, no decorative UI mockup, and no
unrelated mascot or stock imagery. Prefer a transparent background for the
icon source and avoid lossy conversion for the shipping ICO.

## Verification

Build the GUI in Release and check that `ostom.ico` is present beside the
published executable. Inspect the packaged executable/window at a small taskbar
size and at least one larger shell size; check that the icon is not blurry,
cropped, or replaced by the default application icon. Keep the CLI contract
and unrelated GUI behavior unchanged.
