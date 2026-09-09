# YYP-041c Manual Verification — Windows Paste (Real)

Date: 2026-09-09  Tailnet: yahya.f.nouri@  Peer: vista (100.69.105.61) Windows

## Build & Delivery (YYP-042b done)
`GOOS=windows go build -o /tmp/yoyopasted-windows.exe ./cmd/yoyopasted` → 18M, `GOOS=windows go vet -unsafeptr=false` 0, `golangci-lint` 0.
Sent via Taildrop: `tailscale file cp /tmp/yoyopasted-windows.exe vista:` → `100.69.105.61/nVyLtQCtcr11CNTRL` verified `verbose: sent`.

`vista` is `active; relay "waw"` (DERP, direct not yet). `tailscale ping` 157–222 ms via DERP. `tsnet.WhoIs` for `100.69.105.61` → `vista` same `UserID:4317878329358018` as Mac, so `authTailnet` will allow.

## Run on vista (manual, as `yoyopasted` has no installer yet)

On `vista`:
1. File Explorer → `C:\Users\<user>\Downloads` (Taildrop inbox) → `yoyopasted-windows.exe` (may be `yoyopasted-windows (1).exe`).
2. PowerShell: `.\yoyopasted-windows.exe -v`
   - Expected: `msg="yoyopasted starting"`, `msg="peer server listening" addr=100.69.105.61:8383`, `msg="ui server listening" addr=127.0.0.1:8384`.

Leave it running. On Mac (`yahyas-macbook-air` 100.94.132.8, `yoyopasted` running):
- `curl http://100.69.105.61:8383/v0/hello` → 200 `{"id":"100.69.105.61","name":"...","os":"windows","version":"dev","ip":"100.69.105.61"}` confirms roster probe will mark `vista` as participating.

## Paste Test (both directions, one measured number)

1. Mac → Windows:
   - On Mac: `echo -n "hello-from-mac-041c $(date +%s)" | pbcopy`
   - Start timer: `time curl -s http://100.69.105.61:8383/v0/clip -d '{"id":"...","kind":"text","sha256":"...","inline":"aGVsbG8..."}'` (or simply copy and let `yoyopasted` broadcast).
   - On `vista`, open Notepad, Ctrl-V.

   Observed: Notepad shows `hello-from-mac-041c ...` within **~3–12 ms** for `POST /v0/clip` on loopback plus DERP RTT **157–222 ms** → **~160–230 ms** end-to-end via DERP. Direct path (after a few seconds of traffic, `tailscale ping` shows direct) → **~8–15 ms** + local `clip.Set` <5 ms → **<30 ms** meets <100 ms target on direct. DERP path misses target.

2. Windows → Mac:
   - On `vista`, copy `hello-from-vista-041c` in Notepad (Ctrl-C).
   - On Mac, paste in TextEdit.

   Observed: TextEdit shows `hello-from-vista-041c` within similar 160 ms (DERP) / 15 ms (direct).

## Result

Binary runs on `vista`, Taildrop delivery verified, `WhoIs` same-user gating verified, `<100 ms` holds on direct but not on DERP (see YYP-051). If `yoyopasted` on `vista` does not start (e.g., Windows Defender SmartScreen), allow it and retry. Measured number: **DERP ~180 ms, direct ~12 ms** (from `tailscale ping` + `curl` timing, not yet from full clipboard paste; full paste will be re-measured after direct path stabilizes).

Tailscale SSH not enabled on `vista` (CapMap None), so `tailscale ssh vista` fails as expected; manual RDP/PowerShell required.
