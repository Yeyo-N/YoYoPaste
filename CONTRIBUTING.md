# Contributing

See `docs/STYLE.md` for style and `docs/ARCHITECTURE.md` for architecture.

## Worker protocol

1. Pick highest priority `Ready` task whose dependencies are `Done`.
2. Branch `yyp-NNN-short-slug`.
3. Follow STYLE.md ladder.
4. One task, one PR.

Run before pushing:

```bash
gofmt -s -w .
go vet ./...
go test -race ./...
```
