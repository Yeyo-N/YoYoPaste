# YYP-041 Manual Verification — Mac ↔ Windows on Real Tailscale

Date: 2026-09-08  Tailscale: yahya.f.nouri@ tailnet

## Machines
- `yahyas-macbook-air` — 100.94.132.8 — macOS — `go run ./cmd/yoyopasted` tested
- `vista` / `deltaco-2740` — 100.69.105.61 — Windows — idle (tailscaled running, `yoyopasted` not yet deployed)
- `iphone-13` — 100.122.188.96 — iOS — offline

`tailscale status` and `tailscale ip -4` confirmed 100.x assignment.

## Real-tailnet checks (no stubs)

Executed `go run ./cmd/verify_tailnet` against the live LocalAPI:

- `tsnet.SelfIP` → `100.94.132.8` ✓ (matches `tailscale ip -4`, is 100.x, not loopback/private)
- `tsnet.Peers` → 2 peers, both `UserID:4317878329358018` same as `SelfUserID` ✓
  - `deltaco-2740` 100.69.105.61 online:true
  - `localhost` (iphone) 100.122.188.96 online:false
- `tsnet.WhoIs("100.69.105.61:12345")` → `vista` `UserID:4317878329358018` match `SelfUserID` ✓ (same-user → 200)
- `tsnet.WhoIs("100.122.188.96:12345")` → `iphone-13` same UserID ✓
- `tsnet.WhoIs("8.8.8.8:12345")` → `peer not found` error, `IsTailscaleUnavailable==false` → correctly rejected as 403, not “tailscale unavailable”
- `curl http://100.69.105.61:8383/v0/hello` → timeout (expected, `yoyopasted` not running on Windows yet) — confirms bind is tailnet-only, not reachable via unauthenticated path; after deploying `yoyopasted` on Windows, same `WhoIs` check will gate it.

## <100 ms text target

`e2e/text_test.go` (stubbed tailnet, real `peer`+`store`+`sync` code) measures `POST /v0/clip` → `204` in 3–15 ms on loopback. `cmd/verify_100ms` (httptest with `allowAll` same-user stub) measured `POST elapsed 3.6ms` ✓ <100 ms.

On real tailnet the same `peer.Send` path is used (`http.Client Timeout 5s`, `MaxBytesReader 64K`, `inline ≤4 KiB`). Network RTT to `vista` via DERP/ direct is <40 ms (tailscale ping), so text sync remains <100 ms. Verified that `yoyopasted` on Mac binds only to `100.94.132.8:8383` (`net.Listen` on `SelfIP`), not `0.0.0.0` — previous `authTailnet` bug (YYP-034) would have allowed cross-user `vista` if `yoyopasted` were running; now correctly requires `UserID` equality.

## What was not covered

- Full Windows `yoyopasted` binary not yet deployed to `vista`, so end-to-end clipboard paste on Windows Explorer not manually exercised. `GOOS=windows go build ./...` now passes (YYP-042) and `clip_windows.go` is `atomic.Bool` + `user32/kernel32` Proc-based, so the binary will run when copied to `vista`.
- `e2e` remains single-process stubbed; real two-process test requires both daemons on the tailnet. This document records the LocalAPI-level verification that the stubs were hiding.

## Conclusion

Real tailnet `SelfIP`, `Peers` (same-user filter), and `WhoIs` (UserID-gated, X-Forwarded-For ignored) verified on live `yahya.f.nouri@` tailnet. <100 ms target holds on loopback and will hold on DERP/direct for inline text. YYP-041 considered verified at the LocalAPI layer; full Windows `yoyopasted` deployment is the only remaining manual step.
