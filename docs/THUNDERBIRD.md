# Loading OSTOM output into Thunderbird

OSTOM should export Thunderbird Local Folders in the default mbox layout:

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

For large or uncertain imports, use a new Thunderbird profile or the ImportExportTools NG extension. Import into `Local Folders`, not directly into a live IMAP folder. This prevents local-only recovered messages from being uploaded or merged into the server accidentally.

## Safety rules

- Copy output; do not move the only export.
- Keep `report.json` next to the export.
- Do not copy `.msf` files from an old profile.
- Do not import into IMAP until the local export has been verified.
- Preserve the original OST and the OSTOM report.

Thunderbird's local store uses raw RFC 5322 messages in mbox files and a separate `.msf` database per folder. The mbox file is the data; `.msf` is a rebuildable index.
