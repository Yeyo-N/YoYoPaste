# YoYoPaste Style Guide (Ponytail)

The governing rule: **the shortest change that actually works.** A PR that deletes code and keeps the tests green is the best kind of PR.

## The ladder — apply in order, stop at the first rung that holds

1. Does this need to exist at all? Speculative need → do not build it. Say so in the PR.
2. Does something in this repo already do it? Reuse it.
3. Does the standard library do it? Use it.
4. Does the platform do it? (HTTP `Range` over a chunking protocol, a SQLite constraint over app-side validation.)
5. Does an already-vendored dependency do it? Use it.
6. Can it be one line? One line.
7. Only then: the minimum code that works.

Understanding the problem is never the lazy part. Read the flow end to end, then climb.

## Hard rules

- **No new dependency** without justification in the PR body. Current allowed set: `tailscale.com`, `modernc.org/sqlite`, `fyne.io/systray`, `oklog/ulid`. Adding to it is an architectural decision — open an issue first.
- No interface with one implementation. No factory. No config value that never changes.
- No `internal/pkg/util` grab-bags. Name packages for what they do: `clip`, `peer`, `store`, `tsnet`.
- Fix causes, not symptoms. Before patching, `grep` every caller of the function you are touching. One guard in the shared function beats a guard in each caller.
- Errors are wrapped with context and returned: `fmt.Errorf("read blob %s: %w", id, err)`. `panic` only in `main` during startup.
- Deliberate shortcuts with a known ceiling get a `ponytail:` comment naming the ceiling and the upgrade path:
  `// ponytail: single mutex over the outbox; shard per-peer if fan-out exceeds ~20 devices`

## Never simplify away

Input validation at trust boundaries · error handling that would lose user data · the `WhoIs` authentication check · accessibility basics in the UI · anything the user explicitly asked for.

## Go

- `gofmt -s` and `go vet` are CI gates. `golangci-lint` with `errcheck, govet, ineffassign, staticcheck, unused` — no other linters.
- Platform code goes behind build tags in files named `*_darwin.go`, `*_windows.go`. No runtime `if runtime.GOOS ==` branching for platform APIs.
- Exported identifiers get a doc comment starting with the identifier's name. Unexported ones get a comment only when the *why* is non-obvious; never restate the code.
- Table-driven tests. Standard `testing` only — no testify, no ginkgo, no mock framework.

## Swift (iOS)

- SwiftUI, no storyboards. `URLSession` with `async/await`, no networking library.
- `swift-format` default config, enforced in CI.

## Commits — Conventional Commits

```
<type>(<scope>): <imperative summary, <=72 chars>

<why, wrapped at 80 — not what, the diff shows what>

Refs: YYP-014
```
Types: `feat fix docs style refactor test chore ci`. Scopes: `core peer store clip ui ios win mac docs ci`.

## API + docs

- Protocol changes update `docs/ARCHITECTURE.md` §2 **in the same PR**. An undocumented endpoint is a bug.
- Prose: second person, active voice, present tense. Short sentences. No marketing adjectives in technical docs.
- Every code block in a doc must be runnable as written.

## PR checklist

- [ ] Climbed the ladder; PR body names anything deliberately skipped and when to add it
- [ ] `gofmt -s`, `go vet`, `golangci-lint`, `go test ./...` pass
- [ ] Doc updated if behaviour or protocol changed
- [ ] No new dependency, or justification included
- [ ] Diff is as small as the problem allows
