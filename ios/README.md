# YoYoPaste iOS

Builds with Xcode 16+ on macOS with `iphone-13` (100.122.188.96, offline 87d) on the tailnet.

## Build

```bash
xcodebuild -project YoYoPaste.xcodeproj -scheme YoYoPaste -destination 'platform=iOS,name=iphone-13' build
```

Or open `ios/YoYoPaste.xcodeproj` in Xcode and Run on `iphone-13`.

The app bootstraps from the QR in the desktop UI (`GET /v0/peers` via `http://<desktop 100.x>:8383`), lists desktop peers, and `POST /v0/clip` with `URLSession` `async/await`. No background clipboard polling — foreground and share extension only (D13).

## Share Extension

`ios/YoYoPasteShare` — `NSExtensionItem` handling for text, URLs, images, files. Streams from `fileURL`, never `Data(contentsOf:)`, so a 1 GB file stays under 120 MB.

## Notes

- `iphone-13` has been offline 87 days per `tailscale status`; it will need to be brought online and have the Tailscale iOS app logged into `yahya.f.nouri@` tailnet before testing.
- `ContentView.swift` is the 4-element view (toggle, link, table) as per ARCHITECTURE.md §5.
