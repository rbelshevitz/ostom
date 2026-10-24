# Loading OSTOM output into Thunderbird

[en](THUNDERBIRD.md) | [es](THUNDERBIRD.es.md) | [fr](THUNDERBIRD.fr.md) | [de](THUNDERBIRD.de.md) | [nl](THUNDERBIRD.nl.md) | [ja](THUNDERBIRD.ja.md) | [ru](THUNDERBIRD.ru.md) | [he](THUNDERBIRD.he.md)

## The main idea: local first, IMAP second

OSTOM does not upload recovered mail to a server. It produces a normal folder
tree that Thunderbird can open:

```text
PST/OST → OSTOM → Thunderbird Local Folders → IMAP account folders
```

Once the local result is checked, use Thunderbird to upload the messages to
IMAP. Create the needed server folders, then copy or move messages from
`Local Folders` into them. Nested folders work the same way. This lets you
reorganize recovered mail even when OSTOM could not identify the original
folder for every message.

Check the local copy first. Starting with IMAP can synchronize a mistaken move,
run into server quotas, or make the result harder to find during a sync error.

In the Thunderbird interface, this area may have a localized name. The profile
directory on disk is normally named `Local Folders`; use that filesystem name
when copying the export manually.

OSTOM exports Thunderbird Local Folders in the standard mbox layout:

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

Each mailbox is a file with no extension. A folder that has children has both:

```text
FolderName       # mbox message store
FolderName.sbd/  # child folders
```

Thunderbird creates `.msf` index files. OSTOM must not generate `.msf`; stale indexes can hide or miscount messages.

## Recommended import

1. Create or open a Thunderbird profile.
2. Start Thunderbird once so `Local Folders` exists.
3. Close Thunderbird completely.
4. Back up the Thunderbird profile.
5. Copy the contents of OSTOM's `Local Folders` output into the profile's `Mail/Local Folders/` directory.
6. Start Thunderbird.
7. Let Thunderbird rebuild the folder indexes.

The profile is normally under:

```text
%APPDATA%\Thunderbird\Profiles\<profile>\
```

For large or uncertain imports, use a new Thunderbird profile or the
ImportExportTools NG extension. Import into `Local Folders`, not directly into
a live IMAP folder. This keeps unverified recovered messages off the server.

## Uploading verified mail to IMAP

1. Add or open the target IMAP account in Thunderbird.
2. Create the target folders and subfolders there.
3. Select verified messages or a folder under `Local Folders`.
4. Drag them to the IMAP folder, or use Thunderbird's copy/move commands.
5. Wait for synchronization and verify several messages, attachments, and
   nested folders through webmail or another client.

For the first pass, copy rather than move so the local export remains a backup
copy. Check IMAP quotas, attachment limits, and the server's folder-naming
rules.

## Safety rules

- Copy output; do not move the only export.
- Keep `report.json` next to the export.
- Do not copy `.msf` files from an old profile.
- Do not import into IMAP until the local export has been verified.
- Preserve the original OST and the OSTOM report.

Thunderbird's local store uses raw RFC 5322 messages in mbox files and a separate `.msf` database per folder. The mbox file is the data; `.msf` is a rebuildable index.
