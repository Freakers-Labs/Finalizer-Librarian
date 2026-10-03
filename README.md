# Finalizer Librarian

A lightweight Windows utility for backing up, organizing and restoring RAM presets on TC Electronic Finalizer Plus and Finalizer 96k hardware.

**Version 1.3 · Windows 10/11 x64 · Offline application**

[日本語](README.ja.md) · [Privacy](PRIVACY.md) · [License](LICENSE)

This is an independent project, not an official TC Electronic product.

## Features

- Receive and restore complete RAM banks through MIDI SysEx.
- Save backups as standard MIDI files (`.mid`).
- View all 128 slots in a four-column preset list.
- Double-click a slot to send Program Change.
- Rename stored presets and track names not yet confirmed on the hardware.
- Export the preset list as a text file.
- Select MIDI IN and OUT ports and channels independently.
- Keep MIDI driver operations in a separate process with cancellation and timeouts.

No Plus/96k format conversion, parameter editing, meters or continuous hardware synchronization is included.

## Download and run

Download the Windows x64 portable ZIP from this repository's Releases page. Extract all files and run `Finalizer_Librarian_v1.3.exe`. No installer or additional application runtime is required. Install the normal driver for your MIDI interface if needed.

The application starts a second instance of its executable for MIDI operations. Seeing two processes is expected. Neither process connects to the internet.

The executable is unsigned. Windows 11 Smart App Control or other security software may block it. This release does not change Windows protection settings or promise to bypass them. See [Microsoft's Smart App Control guidance](https://support.microsoft.com/en-us/windows/security/threat-malware-protection/smart-app-control-frequently-asked-questions). Source is provided for inspection and local building; building locally is not a guarantee that Windows will allow execution.

## MIDI setup

1. Connect the Finalizer's MIDI OUT to the interface's MIDI IN and its MIDI IN to the interface's MIDI OUT.
2. Select the corresponding ports in the application.
3. Set **OUT CH** to the Finalizer's receive channel and **IN CH** to its transmit channel.
4. Set the hardware's **Program Bank = RAM** and **Offset = 0**.
5. Click **Connect**.

A port can be `(None)` for one-direction use. Click **Refresh** while disconnected to refresh the port list.

OUT CH controls outgoing Program Change. Incoming Program Change matching IN CH selects a row without echoing the message to MIDI OUT. It does not update names or read parameter values.

Bulk dumps use SysEx. CH settings do not filter or rewrite the dump's device-ID byte.

## Receive and save a backup

1. Connect MIDI IN and click **Receive**.
2. Follow the prompt and select **Utility > MEM to MIDI** on the Finalizer.
3. Wait for the receive-complete message and updated preset list.
4. Choose **File > Save** or **File > Save As...** to save a `.mid` file.

**Save explicitly before closing. There is no automatic backup or recovery file.** Unsaved bank data is kept in memory and is lost if the application or computer exits unexpectedly. Normal closing prompts you to save unsaved changes.

Cancel or Esc stops receiving. An incomplete or invalid transfer does not replace the previously loaded bank.

## Select and recall presets

Clicking a row or navigating with the keyboard only selects it. **Double-click** sends Program Change using MIDI OUT. Empty slots in a loaded bank are not recalled. When no bank is loaded, double-click can still recall a slot by number.

Names describe the received or opened file, not a live view of the hardware. Opening a file does not transfer it to the device.

The list stays in four columns. Narrow windows place MIDI settings on two rows. Use the vertical scrollbars for hidden slots and horizontal scrollbars for long names, or maximize the window.

## Rename presets

1. Select a slot containing a preset.
2. Click **Rename**.
3. Edit the name directly in the selected row.
4. Press Enter or move focus outside the field to apply. Press Esc to cancel.

Resizing the window cancels an unfinished name edit. Empty slots cannot be renamed.

Names accept 1-19 printable ASCII characters; blank names and non-ASCII input are rejected. The name field is space-padded and NUL-terminated. Parameter data is preserved and bulk checksums are recalculated.

- A `*` beside a name means the local rename has not been confirmed on the hardware.
- A `*` in the window title means the bank has unsaved file changes.
- Saving the file does not update the hardware or clear the name marker.
- Program Change recalls a slot; it does not write a renamed preset.

Name markers are remembered on this PC using a bank-content hash and slot flags. They contain no preset names or bank payload and do not travel inside the MIDI file. An absent marker is not proof that the connected hardware matches a file.

## Restore a bank to the hardware

**Send overwrites the entire RAM bank, slots 001-128.** Back up the destination bank before restoring. Use the same source and destination model; cross-model restoration is not validated.

1. Open the intended backup using **File > Open...**.
2. Connect MIDI OUT and click **Send**.
3. Select **Utility > MIDI to MEM** on the Finalizer.
4. When the hardware is ready, click **Start Send**.
5. Check the completion display on the Finalizer.

If renamed presets have pending markers, the application asks whether the hardware restore succeeded. Yes clears the current bank's markers; No keeps them. The default is No. MIDI transmission completion alone is not a hardware acknowledgement or readback verification.

Cancelling or failing a send retains pending markers. Cancelling a send disconnects MIDI; inspect the hardware and reconnect before retrying. Sending does not save the local file.

## Export a preset list

Choose **File > Export Preset List as Text...**. The UTF-8 `.txt` file contains all 128 slot numbers, names, empty slots and pending-name markers. It contains your preset names, so review it before sharing. Exporting does not clear unsaved or pending states.

## Files and local privacy

The application has no telemetry, update checker, persistent activity log or automatic dump cache. The distribution contains no user banks, device settings, capture files or private test logs.

The application stores only channel settings, hashed port identifiers, and hashed-bank/slot-marker state in `%LOCALAPPDATA%\FinalizerLibrarian`. Port labels and preset names are not stored in those state files. Hashes support local matching; they are not encryption or a guarantee of anonymity.

Files explicitly saved or exported contain user data by design. Save uses a temporary file in the selected destination folder before replacing the target. See [PRIVACY.md](PRIVACY.md) for details, including existing files from older installations.

## Formats and error handling

- Saves SMF Format 0, one track, tempo and SysEx events; no Program Change is added.
- Reads Format 0/1 PPQN MIDI containing one complete Finalizer RAM bank, including split F0/F7 messages.
- Can import the stored bulk bank from a legacy `.f96proj` file. Editor settings are ignored; save as `.mid` afterward.
- Rejects multiple banks, missing or duplicated packets, inconsistent IDs, bad checksums, SMPTE timing, Format 2 and files larger than 8 MiB.
- Uses a minimum 100 ms gap after the bulk header and 60 ms between subsequent packet starts. Longer imported gaps are retained up to 60 seconds per event. Live receive uses fixed safe spacing, not recorded arrival timing.
- Validates an SMF round trip and temporary-file contents before replacing a saved file.
- Stops an unresponsive MIDI worker after 5 seconds while retaining the UI's bank data. Receive waits up to 120 seconds initially and stops after a 2.5-second packet stall. Individual SysEx send completion waits up to 2 seconds.

The application cannot recover a malfunctioning operating-system driver itself. Check the interface and hardware if reconnecting fails.

## Build and test

Use Go 1.23 or newer. No external Go modules are required. On Windows, run `build.cmd`; output is placed in `dist`.

To build from another Go development environment:

```sh
go test ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w -H windowsgui" -o dist/Finalizer_Librarian_v1.3.exe .
```

Builds disable VCS stamping and trim local source paths. Tests create synthetic banks in memory and do not read personal captures or require MIDI hardware. The build tools may download a Go toolchain depending on your local Go configuration; the built application does not use the network.

`package_release.py` uses an explicit file allowlist to produce source and portable archives. See [RELEASING.md](RELEASING.md).

## Verification and limitations

Automated tests cover bank validation, SMF round trips, malformed input, name edits, layout geometry and privacy of saved port settings. A successful cross-build or geometry test does not verify Windows rendering or physical MIDI behavior. This release's Windows UI and hardware transfers require testing on the intended setup. It is not manufacturer-certified.

## Reporting issues

Include the application version, Windows version, display resolution/scaling, MIDI interface and hardware model/firmware, plus steps to reproduce. Remove personal paths and names from screenshots. Do not attach full banks or production presets; reproduce with a newly created generic preset where possible.

## License

The project is provided under the [MIT License](LICENSE), without warranty. Go runtime and standard-library notices are included in [GO-LICENSE.txt](GO-LICENSE.txt) and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
