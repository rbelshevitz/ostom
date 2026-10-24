# OSTOM-Ausgabe in Thunderbird laden

[en](THUNDERBIRD.md) | [es](THUNDERBIRD.es.md) | [fr](THUNDERBIRD.fr.md) | [de](THUNDERBIRD.de.md) | [ru](THUNDERBIRD.ru.md) | [he](THUNDERBIRD.he.md)

## Der zentrale Gedanke: zuerst lokal, dann IMAP

OSTOM lädt wiederhergestellte Nachrichten nicht auf einen Server hoch. Es
erzeugt eine Ordnerstruktur, die Thunderbird öffnen kann:

```text
PST/OST → OSTOM → Lokale Ordner → Ordner des IMAP-Kontos
```

In der deutschen Thunderbird-Oberfläche heißt dieser Bereich „Lokale Ordner“.
Im Profil heißt der Ordner auf der Festplatte normalerweise `Local Folders`.

Nachdem Sie das lokale Ergebnis geprüft haben, können Sie die Nachrichten mit
Thunderbird zu IMAP hochladen. Erstellen Sie die benötigten Ordner auf dem
Server und kopieren oder verschieben Sie die Nachrichten aus `Local Folders`
dorthin. Unterordner funktionieren genauso. So können Sie die wiederhergestellten
Nachrichten neu ordnen, auch wenn OSTOM den ursprünglichen Ordner einer Nachricht
nicht ermitteln konnte.

Prüfen Sie zuerst die lokale Kopie. Wenn Sie direkt mit IMAP beginnen, kann eine
falsche Verschiebung mit dem Server synchronisiert werden. Außerdem können
Serverquoten oder ein Synchronisierungsfehler den Zugriff erschweren.

OSTOM exportiert die lokalen Thunderbird-Ordner in der standardmäßigen mbox-Struktur:

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

Jedes Postfach ist eine Datei ohne Erweiterung. Ein Ordner mit Unterordnern
besteht aus beiden Elementen:

```text
FolderName       # mbox-Nachrichtenspeicher
FolderName.sbd/  # Unterordner
```

Thunderbird erstellt die Indexdateien `.msf`. OSTOM darf keine `.msf`-Dateien
erzeugen; alte Indizes können Nachrichten verbergen oder falsche Mengen anzeigen.

## Empfohlener Import

1. Erstellen oder öffnen Sie ein Thunderbird-Profil.
2. Starten Sie Thunderbird einmal, damit „Lokale Ordner“ angelegt wird.
3. Beenden Sie Thunderbird vollständig.
4. Sichern Sie das Thunderbird-Profil.
5. Kopieren Sie den Inhalt des von OSTOM exportierten Ordners `Local Folders`
   in `Mail/Local Folders/` im Profil.
6. Starten Sie Thunderbird.
7. Warten Sie, bis Thunderbird die Ordnerindizes neu erstellt hat.

Das Profil befindet sich normalerweise hier:

```text
%APPDATA%\Thunderbird\Profiles\<profile>\
```

Verwenden Sie für große oder unsichere Importe ein neues Thunderbird-Profil
oder die Erweiterung ImportExportTools NG. Importieren Sie zuerst in „Lokale
Ordner“ und nicht direkt in einen aktiven IMAP-Ordner. So verhindern Sie, dass
ungeprüfte wiederhergestellte Nachrichten versehentlich auf den Server gelangen.

## Geprüfte Nachrichten in IMAP hochladen

1. Fügen Sie das Zielkonto hinzu oder öffnen Sie es in Thunderbird.
2. Erstellen Sie dort die benötigten Zielordner und Unterordner.
3. Wählen Sie geprüfte Nachrichten oder einen Ordner unter „Lokale Ordner“ aus.
4. Ziehen Sie sie in den IMAP-Ordner oder verwenden Sie die Kopier- bzw.
   Verschiebebefehle von Thunderbird.
5. Warten Sie die Synchronisierung ab und prüfen Sie mehrere Nachrichten,
   Anhänge und Unterordner im Webmail oder in einem anderen Client.

Kopieren Sie beim ersten Durchlauf lieber, statt zu verschieben, damit der
lokale Export als Sicherungskopie erhalten bleibt. Beachten Sie IMAP-Quoten,
Größenlimits für Anhänge und die Regeln des Servers für Ordnernamen.

## Sicherheitsregeln

- Kopieren Sie das Ergebnis; verschieben Sie nicht die einzige Exportkopie.
- Bewahren Sie `report.json` neben dem Export auf.
- Kopieren Sie keine `.msf`-Dateien aus einem alten Profil.
- Importieren Sie erst in IMAP, nachdem Sie den lokalen Export geprüft haben.
- Bewahren Sie die ursprüngliche OST-Datei und den OSTOM-Bericht auf.

Thunderbirds lokaler Speicher verwendet unveränderte RFC-5322-Nachrichten in
mbox-Dateien sowie eine eigene `.msf`-Datenbank für jeden Ordner. Die mbox-Datei
enthält die Daten; `.msf` ist ein Index, der neu erstellt werden kann.
