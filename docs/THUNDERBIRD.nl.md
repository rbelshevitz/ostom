# OSTOM-uitvoer in Thunderbird laden

[en](THUNDERBIRD.md) | [es](THUNDERBIRD.es.md) | [fr](THUNDERBIRD.fr.md) | [de](THUNDERBIRD.de.md) | [nl](THUNDERBIRD.nl.md) | [ja](THUNDERBIRD.ja.md) | [ru](THUNDERBIRD.ru.md) | [he](THUNDERBIRD.he.md)

## Het belangrijkste idee: eerst lokaal, daarna IMAP

OSTOM uploadt herstelde berichten niet rechtstreeks naar een server. Het maakt
een normale mappenstructuur die Thunderbird kan openen:

```text
PST/OST → OSTOM → Lokale mappen → mappen van het IMAP-account
```

In de Nederlandse Thunderbird-interface heet dit onderdeel „Lokale mappen”. In
het profiel heet de map op de schijf meestal `Local Folders`.

Nadat u het lokale resultaat hebt gecontroleerd, kunt u Thunderbird gebruiken om
de berichten naar IMAP te uploaden. Maak de benodigde mappen op de server en
kopieer of verplaats de berichten vanuit `Local Folders` daarheen. Submappen
werken op dezelfde manier. Zo kunt u herstelde berichten opnieuw ordenen, ook
als OSTOM de oorspronkelijke map van een bericht niet kon vaststellen.

Controleer eerst de lokale kopie. Als u direct met IMAP begint, kan een verkeerde
verplaatsing met de server worden gesynchroniseerd. Ook kunnen serverquota of een
fout tijdens de synchronisatie het resultaat moeilijker vindbaar maken.

OSTOM exporteert de lokale Thunderbird-mappen in de standaard-mboxstructuur:

```text
output/
├── Local Folders/
│   ├── Local Sent
│   ├── Local Sent.sbd/
│   │   ├── 2022
│   │   └── 2023
│   └── Archive
└── report.json
```

Elke mailbox is een bestand zonder extensie. Een map met submappen bestaat uit beide volgende onderdelen:

```text
FolderName       # mbox-opslag voor berichten
FolderName.sbd/  # submappen
```

Thunderbird maakt de indexbestanden `.msf` zelf aan. OSTOM mag geen `.msf`-bestanden
aanmaken; oude indexen kunnen berichten verbergen of onjuiste aantallen tonen.

## Aanbevolen import

1. Maak een Thunderbird-profiel of open een bestaand profiel.
2. Start Thunderbird eenmaal zodat „Lokale mappen” verschijnt.
3. Sluit Thunderbird volledig af.
4. Maak een back-up van het Thunderbird-profiel.
5. Kopieer de inhoud van de door OSTOM geëxporteerde map `Local Folders` naar
   `Mail/Local Folders/` in het profiel.
6. Start Thunderbird.
7. Wacht totdat Thunderbird de mapindexen opnieuw heeft opgebouwd.

Het profiel staat meestal hier:

```text
%APPDATA%\Thunderbird\Profiles\<profile>\
```

Gebruik voor grote of onzekere imports een nieuw profiel of de extensie
ImportExportTools NG. Importeer eerst in „Lokale mappen” en niet rechtstreeks in
een actieve IMAP-map. Zo voorkomt u dat niet-gecontroleerde herstelde berichten
per ongeluk op de server terechtkomen.

## Gecontroleerde berichten naar IMAP uploaden

1. Voeg het doelaccount toe aan Thunderbird of open het.
2. Maak daar de benodigde doelmappen en submappen.
3. Selecteer gecontroleerde berichten of een map onder „Lokale mappen”.
4. Sleep ze naar de IMAP-map of gebruik de kopieer- en verplaatsopdrachten van
   Thunderbird.
5. Wacht totdat de synchronisatie is voltooid en controleer meerdere berichten,
   bijlagen en submappen in webmail of een andere e-mailclient.

Kopieer bij de eerste overdracht liever dan dat u verplaatst, zodat de lokale
export als back-up behouden blijft. Houd rekening met IMAP-quota, limieten voor
de grootte van bijlagen en de regels van de server voor mapnamen.

## Veiligheidsregels

- Kopieer het resultaat; verplaats niet de enige export.
- Bewaar `report.json` naast de export.
- Kopieer geen `.msf`-bestanden uit een oud profiel.
- Importeer pas naar IMAP nadat u de lokale export hebt gecontroleerd.
- Bewaar het oorspronkelijke OST-bestand en het OSTOM-rapport.

De lokale opslag van Thunderbird gebruikt de oorspronkelijke RFC 5322-berichten in
mbox-bestanden en een aparte `.msf`-database voor elke map. Het mbox-bestand bevat
de gegevens; `.msf` is een index die opnieuw kan worden opgebouwd.
