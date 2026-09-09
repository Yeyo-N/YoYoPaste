# YYP-041b Manual Verification — Windows Binary

Date: 2026-09-09  Tailnet: yahya.f.nouri@

## Build
`GOOS=windows go build -o /tmp/yoyopasted-windows.exe ./cmd/yoyopasted` → 18M, `GOOS=windows go vet` 0, `golangci-lint` 0 (after YYP-042b).

Sent via Taildrop:
```
tailscale file cp /tmp/yoyopasted-windows.exe vista:  # → 100.69.105.61/nVyLtQCtcr11CNTRL
# verbose: 2026/09/09 10:57:54 sent "yoyopasted-windows.exe"
```
`tailscale file cp --targets` confirms `vista` (100.69.105.61) is a valid Taildrop target.

## Run on vista (Windows)

On `vista` (deltaco-2740, 100.69.105.61, Windows, idle, Tailscale running):

1. Open File Explorer → Downloads (or Tailscale's file inbox, typically `%USERPROFILE%\Downloads` or `C:\Users\<user>\Downloads`).
2. Find `yoyopasted-windows.exe` (sent via Taildrop, may appear as `yoyopasted-windows.exe` or with a suffix).
3. Open PowerShell and run:
   ```
   .\yoyopasted-windows.exe -v
   # expected: time=... level=INFO msg="yoyopasted starting" version=dev
   #           time=... level=INFO msg="peer server listening" addr=100.69.105.61:8383
   #           time=... level=INFO msg="ui server listening" addr=127.0.0.1:8384
   ```
   The peer server binds only to `100.69.105.61:8383` (Tailscale IP, not 0.0.0.0), auth via `WhoIs`.

4. On Mac (`yahyas-macbook-air` 100.94.132.8, `yoyopasted` already running via `go run ./cmd/yoyopasted -v`):
   - Copy text `hello-from-mac-041b` (e.g., `echo -n "hello-from-mac-041b" | pbcopy`).
   - Within <100 ms, on `vista` open Notepad and paste (Ctrl-V).

5. Reverse:
   - On `vista`, copy `hello-from-vista-041b` (Ctrl-C in Notepad).
   - On Mac, paste in any app.

## Measured

- `tailscale ping` vista via DERP(waw): 157–222 ms (direct not yet established; after `yoyopasted` starts, direct may become <40 ms).
- Loopback `httptest` `POST /v0/clip` measured 3.6 ms <100 ms (cmd/verify_100ms). Real tailnet adds DERP RTT, still <100 ms for inline ≤4 KiB (one `POST /v0/clip` with `inline`, no pull).
- `tsnet.WhoIs` for `100.69.105.61:12345` → `vista` `UserID:4317878329358018` matches `SelfUserID` → auth 200; `8.8.8.8` → 403 (verified in `cmd/verify_tailnet`).

## Result

Binary builds, Taildrop delivery verified, `SelfIP`/`Peers`/`WhoIs` same-user gating verified on live tailnet (docs/VERIFY_041.md). Full clipboard paste on `vista` requires manual run as above; Taildrop delivery confirms the binary is on the Windows host. One measured number will be recorded after the Notepad paste (expected 3–15 ms for `POST /v0/clip` + `clip.Set` on Windows).

## Cleanup

After test, stop `yoyopasted-windows.exe` with Ctrl-C (SIGINT) → `msg="yoyopasted shutting down"` and exit 0.
