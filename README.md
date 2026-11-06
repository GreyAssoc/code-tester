# codeintegrity

Multi-language deterministic code integrity assessor. Detects languages under a
tree and runs per-language drivers (Go, Python, JS/TS, CSS), merging one report.
The same binary also exposes the deep Go surface (check, flow graphs, schema, suppress).

Go-only design history: `go-code-integrity-design.md`. Architecture:
`docs/ARCHITECTURE.md`. Roadmap: `docs/ROADMAP.md`.

**Version:** `0.4.0` (source: `report.ToolVersion` in `report/model.go`)

## Build

```sh
go build -o codeintegrity ./cmd/codeintegrity
```

## Usage

```sh
# Multi-language crawl
./codeintegrity -C path/to/repo
./codeintegrity --lang go,python,js,css --format json -o report.json .
./codeintegrity --lang python ./pyapp
./codeintegrity languages

# Deep Go crawl (package patterns or Go flags)
./codeintegrity --no-config --roots main,init,exported ./...
./codeintegrity crawl --flow --format dot -o flow.dot ./...

# Go check / tooling
./codeintegrity check --request checks.json ./...
./codeintegrity rules
./codeintegrity schema report
./codeintegrity suppress --rule FLOW001 --source SRC --sink SNK --reason TEXT --owner WHO
./codeintegrity version
```

Dogfood the tool on this repo (defaults already exclude `testdata/**`, `node_modules/**`, `vendor/**`, `.git/**`, `dist/**`, `build/**`):

```sh
./codeintegrity --fail-on error .
# or deep Go only:
./codeintegrity --no-config --roots main,exported ./...
```

Keep `testdata/` in the tree — integration tests and multi-lang fixtures need it. Do not delete it; defaults skip it when crawling the repo root.

## Layout

| Package | Role |
|---|---|
| `assessor/` | language detection, dispatch, merge |
| `lang/` | Driver interface |
| `lang/go` | Go SSA/RTA engine |
| `lang/python` (+ `engine`) | Python integrity engine |
| `lang/js` (+ `engine`) | JS/TS integrity engine |
| `lang/css` (+ `engine`) | CSS integrity engine |
| `loader/`, `callgraph/`, `rules/`, `flow/`, `check/` | Go analysis core |
| `cmd/codeintegrity` | sole CLI |

## Tests

```sh
go test ./...
```

CI (GitHub Actions): `go test ./...` and a self dogfood crawl (`./codeintegrity --fail-on error .`) on every push/PR to `main`. See `.github/workflows/ci.yml`.
