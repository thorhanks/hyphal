# Key probe

Browsers keep some keys for themselves, and automated tests can't see that: they send keys past the browser's own shortcuts. The key probe records by hand which of Hyphal's key bindings actually reach the page (SPEC §6.4, "Human key matrix").

## Run it

The probe is only in development builds, run with `--dev`:

```bash
go build -tags dev -o /tmp/hyphal-dev ./cmd/hyphal
```

```bash
HYPHAL_DB=/tmp/keyprobe/outline.db XDG_DATA_HOME=/tmp/keyprobe XDG_RUNTIME_DIR=/tmp/keyprobe/run /tmp/hyphal-dev serve --dev
```

The temporary paths keep the probe away from your real outline. The server prints a launch URL. Open it in the browser you want to test, then change the address to `/keyprobe` on the same host and port.

## Record

1. Click the "Press keys here" box.
2. Press every key in the Bindings table, then every key in "Reserved by the browser". A key that reaches the page shows "yes". A key the browser takes (a new tab, closing the window) never shows up; reopen the probe if the browser acts on it.
3. Some keys are bound in several contexts. One press marks all of its rows.
4. Click "Download result". The JSON file holds every binding with `reached` true or false, the reserved keys, any other keys pressed, the user agent and the keymap platform.

Record these, as P0-23 asks:

- Chromium in a normal tab
- Chromium as an app window (`--app=<url>`); `standalone` in the result is true when the browser reports it
- Firefox
- Safari on macOS

## Read the result

- A non-native binding with `reached: false` is a key the browser keeps. It needs another default, or a note in SPEC §6.4.
- A reserved key with `reached: true` is fine: it only means that browser gives it up.
- Native bindings (`native: true`) are performed by the browser. The page never cancels them, so they only need to reach the page.
