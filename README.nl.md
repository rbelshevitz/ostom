# OSTOM

[en](README.md) | [es](README.es.md) | [fr](README.fr.md) | [de](README.de.md) | [nl](README.nl.md) | [ja](README.ja.md) | [ru](README.ru.md) | [he](README.he.md)

> Soms bewaart Outlook te veel berichten.

[![CI](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml/badge.svg)](https://github.com/rbelshevitz/ostom/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/doc/install)
[![.NET](https://img.shields.io/badge/.NET-8.0-512BD4?logo=dotnet&logoColor=white)](https://dotnet.microsoft.com/download/dotnet/8.0)

OSTOM is een hulpprogramma voor herstel in alleen-lezenmodus. Het herstelt
leesbare berichten uit PST-/OST-bestanden van Outlook en exporteert ze naar mbox en EML.

OSTOM leest Outlook-gegevensbestanden zonder ze te wijzigen. Als een OST/PST-bestand
maar gedeeltelijk leesbaar is, kopieert OSTOM de herstelbare berichten en bijlagen
naar een Thunderbird-compatibele mbox-mapstructuur, meldt het beschadigde delen
en laat het bronbestand ongewijzigd.

Kort gezegd is OSTOM een lokaal herstelprogramma met een duidelijk rapport. Het
repareert en reconstrueert geen PST/OST-bestanden. Herstelde berichten en bijlagen
worden opgeslagen in open exportformaten.

<p align="center">
  <img src="docs/accountant-recovered-mail.png"
       alt="Een boekhouder viert herstelde e-mail"
       width="900">
</p>

- de normale indexgebaseerde export behoudt leesbare mappen en berichten;
- een afzonderlijke herstelroute kan extra berichten vinden wanneer de index
  beschadigd is;
- berichten en bijlagen worden tijdens het lezen geschreven, zodat één beschadigd
  onderdeel het overige leesbare resultaat niet aantast;
- het rapport en het manifest maken onderscheid tussen geëxporteerde, gedeeltelijke,
  mislukte en dubbele resultaten.

## Snel starten

Werk met een kopie van het bestand en bewaar het origineel ongewijzigd.

Het belangrijkste idee is eenvoudig: open het resultaat van OSTOM in Thunderbird
en orden de berichten daar in IMAP-mappen. Laad de export eerst in de lokale map
`Lokale mappen` van Thunderbird en controleer de inhoud. In het profiel heet deze
map op de schijf meestal `Local Folders`. Maak daarna de benodigde IMAP-mappen en
kopieer of verplaats de berichten daarheen. OSTOM herstelt leesbare gegevens;
Thunderbird toont ze en kan ze naar de server uploaden. OSTOM maakt zelf geen
verbinding met IMAP en wijzigt het bronbestand niet.

De structuur controleren en een JSON-rapport opslaan:

```powershell
.\ostom.exe -json -output-dir E:\recovered source-copy.ost
```

Leesbare berichten exporteren:

```powershell
.\ostom.exe -export-dir E:\recovered source-copy.ost
```

Als het bestand beschadigd is of de normale route niet volstaat, forceer dan het
afzonderlijke herstelproces:

```powershell
.\ostom.exe -recovery-scan -export-dir E:\recovered source-copy.ost
```

Sluit Thunderbird, kopieer de aangemaakte map `Local Folders` naar het profiel en
controleer de herstelde berichten en bijlagen voordat u ze verder gebruikt. Zie de
[handleiding voor import in Thunderbird](docs/THUNDERBIRD.nl.md) voor de volledige
werkwijze: eerst lokaal, daarna in IMAP.

## Opties

```text
ostom [-json] [-check|-analyze] [-progress-json] [-eml]
      [-output-dir DIR] [-export-dir DIR]
      [-recovery-scan] [-native-only] [-quiet] file.pst|file.ost
```

- `-check` — snelle structuurcontrole zonder de berichtinhoud te lezen;
- `-analyze` — beperkte analyse van de inhoud zonder export;
- `-export-dir DIR` — bestemming voor herstelde berichten. De normale export
  wordt onder `Local Folders` opgeslagen;
- `-output-dir DIR` — afzonderlijke bestemming voor `ostom-report.json` en, bij
  herstel, `recovery-manifest.json`/`.csv`. Als deze optie ontbreekt, worden deze
  bestanden in `-export-dir` opgeslagen;
- `-recovery-scan` — voert een extra herstelproces uit voor beschadigde bestanden;
- `-native-only` — voert alleen het extra herstelproces uit;
- `-eml` — schrijft voor elk hersteld bericht een apart `.eml`-bestand naar elk
  geselecteerd exportpad;
- `-json` — schrijft het eindrapport als JSON;
- `-progress-json` — schrijft machineleesbare voortgangsgebeurtenissen naar stderr;
- `-quiet` — onderdrukt de voortgangstekst.

`-recovery-scan` en `-native-only` vereisen `-export-dir`.

## Wat u kunt verwachten

OSTOM meldt wat uit dit specifieke bestand kon worden gelezen. Een geslaagde uitvoering
bewijst niet dat er geen onleesbare gegevens aanwezig zijn. Sommige herstelde
berichten behouden mogelijk hun oorspronkelijke map niet. Een leeg mbox-bestand
kan gewoon een lege bronmap vertegenwoordigen. Controleer het rapport voordat u het
bronbestand of de back-up verwijdert.

De Windows-GUI is een eenvoudige interface voor dezelfde CLI. Bewaar
`ostom-gui.exe` en de bijbehorende `ostom.exe` in dezelfde map. De GUI vereist de
.NET 8 Desktop Runtime voor x64.

## Bouwen en documentatie

Go 1.24 of hoger is vereist:

```powershell
go build -trimpath -o ostom.exe ./cmd/ostom
go test -trimpath ./...
```

- [Details over formaat en herstel](docs/FORMAT.md) (Engels)
- [Import in Thunderbird](docs/THUNDERBIRD.nl.md)
- [Ontwikkelplan](docs/TODO.md) (Engels)
- [Licentieverklaringen voor onderdelen van derden](docs/THIRD_PARTY_NOTICES.md) (Engels)

OSTOM is onafhankelijk van Microsoft en verstuurt geen brongegevens via het
netwerk. De gepubliceerde binaire bestanden zijn momenteel niet ondertekend.
Controleer gedownloade archieven met de gepubliceerde SHA-256-hashwaarden.
