# OSTOM

[en](README.md) | [cs](README.cs.md) | [es](README.es.md) | [fr](README.fr.md) | [de](README.de.md) | [nl](README.nl.md) | [ja](README.ja.md) | [ru](README.ru.md) | [he](README.he.md)

> Někdy Outlook ukládá příliš mnoho e-mailů.

[![CI](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml/badge.svg)](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/doc/install)
[![.NET](https://img.shields.io/badge/.NET-8.0-512BD4?logo=dotnet&logoColor=white)](https://dotnet.microsoft.com/download/dotnet/8.0)

OSTOM je nástroj pracující pouze pro čtení, který obnovuje čitelné e-maily ze souborů
Outlook PST/OST a exportuje je do formátů mbox a EML.

OSTOM čte datové soubory Outlooku, aniž by je měnil. Pokud je soubor OST/PST
čitelný jen částečně, zkopíruje dostupné e-maily a přílohy do stromu mbox
kompatibilního s Thunderbirdem, nahlásí poškozené části a ponechá zdrojový
soubor beze změny.

Stručně řečeno: OSTOM je lokální nástroj pro obnovu s transparentním protokolem.
Nevytváří nový „opravený“ soubor PST/OST. Obnovené zprávy a přílohy ukládá
v otevřených exportních formátech.

<p align="center">
  <img src="docs/recovered-mail.png"
       alt="Spokojená účetní slaví obnovené e-maily"
       width="900">
</p>

- běžný indexovaný export zachovává čitelné složky a zprávy;
- samostatný omezený obnovovací průchod může najít další zprávy, pokud je index poškozený;
- zprávy a přílohy se zapisují průběžně, takže jedna chybná položka nezahodí zbytek čitelných dat;
- protokol a manifest rozlišují exportované, částečné, neúspěšné a duplicitní výsledky.

## Rychlý start

Pracujte s kopií zdrojového souboru a originál ponechte beze změny.

Základní myšlenka je jednoduchá: výsledek OSTOM otevřete v Thunderbirdu a
poté v něm uspořádáte zprávy do složek IMAP. Nejprve načtěte export do místních
`Local Folders` v Thunderbirdu a zkontrolujte jej. Potom vytvořte potřebné
složky IMAP a zprávy do nich zkopírujte nebo přesuňte. OSTOM obnovuje čitelná
data; Thunderbird je zobrazí a může je nahrát na server. OSTOM se k IMAP
nepřipojuje a zdrojový soubor PST/OST nemění.

Prohlédnutí struktury a uložení JSON protokolu:

```powershell
.\ostom.exe -json -output-dir E:\recovered source-copy.ost
```

Exportovat čitelné e-maily (protokol se uloží také do exportního adresáře):

```powershell
.\ostom.exe -export-dir E:\recovered source-copy.ost
```

Pokud je soubor poškozený nebo běžný postup nestačí, spusťte samostatný
obnovovací průchod:

```powershell
.\ostom.exe -recovery-scan -export-dir E:\recovered source-copy.ost
```

Zavřete Thunderbird, zkopírujte vytvořený adresář `Local Folders` do jeho
profilu a zkontrolujte obnovené zprávy a přílohy, než se na ně spolehnete.
Úplný postup najdete v [příručce pro import do Thunderbirdu](docs/THUNDERBIRD.md).

## Možnosti

```text
ostom [-json] [-check|-analyze] [-progress-json] [-eml]
      [-output-dir DIR] [-export-dir DIR]
      [-recovery-scan] [-native-only] [-quiet] file.pst|file.ost
```

- `-check` — rychlá kontrola struktury bez čtení obsahu zpráv.
- `-analyze` — omezená analýza obsahu bez exportu.
- `-export-dir DIR` — cíl pro obnovenou poštu. Běžný export se zapisuje pod `Local Folders`.
- `-output-dir DIR` — samostatný cíl pro `ostom-report.json` a při obnově také `recovery-manifest.json`/`.csv`. Pokud není zadán, uloží se tyto soubory do `-export-dir`.
- `-recovery-scan` — spustí další obnovovací průchod pro poškozené soubory.
- `-native-only` — spustí pouze další obnovovací průchod.
- `-eml` — navíc zapíše jeden `.eml` soubor pro každou obnovenou zprávu v každém zvoleném exportním umístění, včetně nativní obnovy.
- `-json` — zapíše konečný protokol jako JSON.
- `-progress-json` — zapíše strojově čitelné události průběhu na stderr.
- `-quiet` — potlačí textový průběh.

`-recovery-scan` a `-native-only` vyžadují `-export-dir`.

## Co očekávat

OSTOM uvádí, co bylo čitelné z tohoto konkrétního souboru. Úspěšné dokončení
není důkazem, že žádná nečitelná data neexistují. Některé obnovené zprávy
nemusí zachovat původní složku. Prázdný soubor mbox může jednoduše představovat
prázdnou zdrojovou složku. Před odstraněním zdroje nebo jeho zálohy si
prohlédněte protokol obnovy.

Grafické rozhraní pro Windows je tenká vrstva nad stejným CLI. `ostom-gui.exe`
a odpovídající `ostom.exe` musí být ve stejném adresáři. GUI vyžaduje systémové
běhové prostředí .NET 8 Desktop Runtime; pokud Windows oznámí, že framework
chybí, nainstalujte x64 verzi .NET 8 Desktop Runtime.

## Sestavení a dokumentace

Je vyžadován Go 1.24 nebo novější:

```powershell
go build -trimpath -o ostom.exe ./cmd/ostom
go test -trimpath ./...
```

- [Podrobnosti formátu a obnovy](docs/FORMAT.md)
- [Import do Thunderbirdu](docs/THUNDERBIRD.md)
- [Plán vývoje](docs/TODO.md)
- [Oznámení o licencích třetích stran](docs/THIRD_PARTY_NOTICES.md)

OSTOM není nijak spojen se společností Microsoft a neposílá zdrojová data přes síť.
Vydané binární soubory zatím nejsou podepsané; stažené archivy ověřte podle
publikovaných kontrolních součtů SHA-256.

Tento nástroj není nijak spojen s žádnou českou ani slovenskou společností.
