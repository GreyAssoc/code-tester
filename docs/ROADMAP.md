# codeintegrity Roadmap

| | |
|---|---|
| **Product** | codeintegrity |
| **Current version** | 0.4.0 |
| **Updated** | 2026-11-04 |

Status values: `todo` · `doing` · `done` · `parked`

## [NOW] High impact

| ID | Item | Status | Notes |
|---|---|---|---|
| R-001 | Kill `internal/poll` SSA panic — bump `golang.org/x/tools` (align Go toolchain); keep recover as safety net | done | `x/tools` → v0.35.0; `go` 1.23.0; loader recover kept |
| R-002 | Default dogfood-friendly excludes (`testdata/**`, `node_modules/**`, `.git/**`) + “excluded N paths” stderr line | done | `config.DefaultExcludes` / `MergeExcludes`; CLI prints `excluded: N path(s)` |
| R-003 | `codeintegrity rules` lists all languages via assessor / `lang/*/Rules()` | done | Registry-driven; optional `--lang` filter |
| R-004 | Module vs product naming — rename module to `codeintegrity` **or** document `gointegrity` module + `codeintegrity` CLI as intentional | todo | Pick one story in ARCHITECTURE |

## [NEXT] Product depth

| ID | Item | Status | Notes |
|---|---|---|---|
| R-010 | Non-Go `check` mode (request / baseline / suppress for Python & JS) | todo | Go-only today |
| R-011 | De-dupe overlapping findings (STUB + COH + DEAD on same symbol) | todo | Demote or suppress when stronger rule fired |
| R-012 | `--mode library\|app` (or smarter default when no `main`) | todo | Quiet library self-crawls without hand-tuned `--roots` |

## [LATER] Repo hygiene

| ID | Item | Status | Notes |
|---|---|---|---|
| R-020 | Gitignore committed report markdowns and built `codeintegrity` binary | todo | Clean working tree |
| R-021 | Single version source (`cmd` const ↔ `report.ToolVersion`) | todo | Avoid drift |
| R-022 | CI: `go test ./...` + dogfood job with `--exclude 'testdata/**' --fail-on error .` | todo | |
| R-023 | Close design §15 (publish path, install, default fail-on) as decisions in ARCHITECTURE | todo | |

## [SOMEDAY] Nice later

| ID | Item | Status | Notes |
|---|---|---|---|
| R-030 | Real parsers for Python/JS (replace regex IR where false negatives hurt) | todo | |
| R-031 | Config schema + `codeintegrity init` writing `.codeintegrity.yml` | todo | |
| R-032 | SARIF/HTML polish + README “dogfood this repo” as primary quickstart | todo | |

## Suggested sequence

1. R-001 → R-002 → R-020 (stability + quiet dogfood)
2. R-003 → R-021 → R-022 (CLI honesty + CI)
3. R-004 → R-023 (naming + product decisions)
4. R-011 → R-012 → R-010 (signal quality, then non-Go check)
5. R-030+ as needed
