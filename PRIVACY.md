# Privacy and local data

## Application behavior

Finalizer Librarian is an offline MIDI utility. It has no network client,
telemetry, analytics, update checker or persistent activity log. MIDI worker
messages travel through local process pipes, not files or network sockets.
Received and opened banks necessarily exist in process memory while in use.

No recovery bank, automatic MIDI backup, recent-file history or preset-name
cache is created. Unsaved banks are lost on unexpected termination.

Two local state files are stored under `%LOCALAPPDATA%\FinalizerLibrarian`:

| File | Contents |
| --- | --- |
| settings.json | IN/OUT channel numbers and SHA-256 hashes of selected MIDI port labels |
| rename-state.json | SHA-256 hashes of bank payloads and 128 boolean pending-name flags per recorded bank |

These files do not contain plaintext device labels, preset names, user-chosen
file paths or bulk payloads. Hashes are identifiers for matching, not encryption;
some identifiers can be guessed or linked by someone who already has candidate
data. Keep local state private even though the original text is not stored.

## Explicitly saved files

MIDI backups contain the complete bank, including preset names and opaque
hardware data. Text exports contain preset names. Share only files you have
reviewed for disclosure.

Atomic saving creates a temporary `.finalizer-*.tmp` file in the destination
folder, writes/verifies the data, then replaces the target. Normal error paths
remove the temporary file. A crash or power loss may leave a temporary file;
it may contain the data being saved. This is not a background cache. Windows
paging, crash reporting, antivirus and other software are outside this app's
control. The application does not promise secure memory or disk erasure.

## Existing installations

Older installations may have left `recovery.mid`, plaintext port labels in
settings, or other local files. This version does not load or create recovery
banks and does not silently delete existing user files. Saving current settings
replaces the settings schema with hashed port identifiers. Reselect ports after
upgrading if necessary.

To remove old state, close the application, inspect the application data folder,
and preserve any backup you still need before deleting it. Deleting
`rename-state.json` forgets the pending-name markers; deleting `settings.json`
resets connection preferences. Do not upload this folder to a public repository.

## Distribution

Official packaging includes only explicitly listed source/documentation files
and the built executable. It excludes banks, MIDI recordings, user settings,
logs, build caches, repository history and editor state. Tests use synthetic
in-memory data. ZIP entries are written without filesystem extended attributes.
