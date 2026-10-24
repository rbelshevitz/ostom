# OSTOM

[en](README.md) | [es](README.es.md) | [fr](README.fr.md) | [de](README.de.md) | [nl](README.nl.md) | [ja](README.ja.md) | [ru](README.ru.md) | [he](README.he.md)

> Outlook speichert viele Nachrichten. OSTOM hilft, sie wiederzufinden.

[![CI](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml/badge.svg)](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/doc/install)
[![.NET](https://img.shields.io/badge/.NET-8.0-512BD4?logo=dotnet&logoColor=white)](https://dotnet.microsoft.com/download/dotnet/8.0)

OSTOM ist ein schreibgeschütztes Werkzeug, das lesbare Nachrichten aus
PST-/OST-Dateien von Outlook wiederherstellt und in die Formate mbox und EML
exportiert.

OSTOM liest Outlook-Datendateien, ohne sie zu verändern. Wenn eine OST/PST-Datei
nur teilweise lesbar ist, kopiert OSTOM die wiederherstellbaren Nachrichten und
Anhänge in eine Thunderbird-kompatible mbox-Ordnerstruktur, weist auf beschädigte
Bereiche hin und lässt die Quelldatei unverändert.

Kurz gesagt: OSTOM ist ein lokales Wiederherstellungswerkzeug mit einem klaren
Bericht. Es repariert und rekonstruiert keine PST/OST-Dateien. Wiederhergestellte
Nachrichten und Anhänge werden in offenen Exportformaten gespeichert.

<p align="center">
  <img src="docs/accountant-recovered-mail.png"
       alt="Eine Buchhalterin freut sich über wiederhergestellte E-Mails"
       width="900">
</p>

- der reguläre indexbasierte Export erhält lesbare Ordner und Nachrichten;
- ein separater Wiederherstellungsweg kann zusätzliche Nachrichten finden,
  wenn der Index beschädigt ist;
- Nachrichten und Anhänge werden beim Lesen geschrieben, sodass ein beschädigtes
  Element den übrigen lesbaren Inhalt nicht beeinträchtigt;
- Bericht und Manifest unterscheiden zwischen exportierten, teilweisen,
  fehlgeschlagenen und doppelten Ergebnissen.

## Schnellstart

Arbeiten Sie mit einer Kopie der Datei und bewahren Sie das Original unverändert
auf.

Der zentrale Gedanke ist einfach: Öffnen Sie das Ergebnis von OSTOM in
Thunderbird und ordnen Sie die Nachrichten dort in IMAP-Ordner ein. Laden Sie
den Export zuerst in den Thunderbird-Bereich „Lokale Ordner“ und prüfen Sie ihn.
Im Profil heißt dieser Ordner auf der Festplatte `Local Folders`. Erstellen Sie
danach die benötigten IMAP-Ordner und kopieren oder
verschieben Sie die Nachrichten dorthin. OSTOM stellt lesbare Daten wieder her;
Thunderbird zeigt sie an und kann sie auf den Server hochladen. OSTOM verbindet
sich nicht mit IMAP und verändert die PST/OST-Quelldatei nicht.

Struktur prüfen und einen JSON-Bericht speichern:

```powershell
.\ostom.exe -json -output-dir E:\recovered source-copy.ost
```

Lesbare Nachrichten exportieren:

```powershell
.\ostom.exe -export-dir E:\recovered source-copy.ost
```

Wenn die Datei beschädigt ist oder der normale Weg nicht ausreicht, erzwingen
Sie den separaten Wiederherstellungsprozess:

```powershell
.\ostom.exe -recovery-scan -export-dir E:\recovered source-copy.ost
```

Schließen Sie Thunderbird, kopieren Sie den erstellten Ordner `Local Folders`
in Ihr Profil und prüfen Sie die wiederhergestellten Nachrichten und Anhänge,
bevor Sie sie weiterverwenden. Der [Leitfaden zum Import in Thunderbird](docs/THUNDERBIRD.md)
beschreibt den vollständigen Ablauf: zuerst lokal, dann im IMAP-Konto.

## Optionen

```text
ostom [-json] [-check|-analyze] [-progress-json] [-eml]
      [-output-dir DIR] [-export-dir DIR]
      [-recovery-scan] [-native-only] [-quiet] file.pst|file.ost
```

- `-check` — schnelle Strukturprüfung, ohne den Nachrichteninhalt zu lesen;
- `-analyze` — begrenzte Inhaltsanalyse ohne Export;
- `-export-dir DIR` — Ziel für wiederhergestellte Nachrichten. Der normale
  Export wird im Ordner `Local Folders` abgelegt;
- `-output-dir DIR` — separates Ziel für `ostom-report.json` und bei einer
  Wiederherstellung für `recovery-manifest.json`/`.csv`. Wenn die Option fehlt,
  werden diese Dateien in `-export-dir` geschrieben;
- `-recovery-scan` — führt einen zusätzlichen Wiederherstellungsprozess für
  beschädigte Dateien aus;
- `-native-only` — führt nur den zusätzlichen Wiederherstellungsprozess aus;
- `-eml` — schreibt für jede wiederhergestellte Nachricht eine eigene `.eml`-
  Datei in jeden ausgewählten Exportpfad;
- `-json` — schreibt den abschließenden Bericht als JSON;
- `-progress-json` — schreibt maschinenlesbare Fortschrittsereignisse nach
  stderr;
- `-quiet` — unterdrückt den Fortschrittstext.

`-recovery-scan` und `-native-only` erfordern `-export-dir`.

## Was Sie erwarten können

OSTOM berichtet, was aus dieser konkreten Datei gelesen werden konnte. Ein
erfolgreicher Lauf beweist nicht, dass keine unlesbaren Daten vorhanden sind.
Einige wiederhergestellte Nachrichten werden möglicherweise nicht ihrem
ursprünglichen Ordner zugeordnet. Eine leere mbox-Datei kann einfach für einen
leeren Quellordner stehen. Prüfen Sie den Bericht, bevor Sie die Quelldatei oder
ihre Sicherung löschen.

Die Windows-GUI ist eine einfache Oberfläche für dasselbe CLI. Bewahren Sie
`ostom-gui.exe` und die zugehörige `ostom.exe` im selben Ordner auf. Die GUI
benötigt die .NET 8 Desktop Runtime für x64.

## Build und Dokumentation

Go 1.24 oder höher ist erforderlich:

```powershell
go build -trimpath -o ostom.exe ./cmd/ostom
go test -trimpath ./...
```

- [Details zu Format und Wiederherstellung](docs/FORMAT.md) (Englisch)
- [Import in Thunderbird](docs/THUNDERBIRD.de.md)
- [Entwicklungsplan](docs/TODO.md) (Englisch)
- [Hinweise zu Drittanbieterkomponenten](docs/THIRD_PARTY_NOTICES.md) (Englisch)

OSTOM ist unabhängig von Microsoft und sendet keine Quelldaten über das Netz.
Die veröffentlichten Binärdateien sind derzeit nicht signiert. Vergleichen Sie
die SHA-256-Hashwerte der heruntergeladenen Archive mit den veröffentlichten
Werten.
