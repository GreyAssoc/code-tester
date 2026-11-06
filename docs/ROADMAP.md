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
| R-004 | Module rename per D-015-09 (`github.com/GreyAssoc/code-tester`) + fix all imports | todo | Decision recorded; implementation pending |

## [NEXT] Product depth

| ID | Item | Status | Notes |
|---|---|---|---|
| R-010 | Non-Go `check` mode (request / baseline / suppress for Python & JS) | todo | Go-only today |
| R-011 | De-dupe overlapping findings (STUB + COH + DEAD on same symbol) | todo | Demote or suppress when stronger rule fired |
| R-012 | `--mode library\|app` (or smarter default when no `main`) | todo | Quiet library self-crawls without hand-tuned `--roots` |
| R-013 | Default `--context=1`; broad FLOW lists; strict FLOW002 (D-015-01..03) | done | context default 1; expanded DefaultSources/Sinks/secrets; FLOW002 error+high |
| R-014 | Windows-aware multi-config Go analysis (D-015-04) | todo | Detect Windows-oriented code; extra GOOS=windows pass on Linux hosts |
| R-015 | Field-level dead code + field-use tracking (D-015-05) | todo | Rule on once tracking is solid |
| R-016 | Chunked SARIF by path-depth bands (D-015-07) | todo | e.g. 0–3, 4–7, 8–10… |
| R-017 | No persisted analysis cache by default (D-015-08) | todo | Confirm CI/docs never recommend stale cache |

## [LATER] Repo hygiene

| ID | Item | Status | Notes |
|---|---|---|---|
| R-020 | Gitignore committed report markdowns and built `codeintegrity` binary | done | `*Code Tester Report.md`, `/codeintegrity`, `codeintegrity.exe` |
| R-021 | Single version source (`cmd` const ↔ `report.ToolVersion`) | done | `report.ToolVersion` only; CLI + tests import it |
| R-022 | CI: `go test ./...` + dogfood job with `--exclude 'testdata/**' --fail-on error .` | done | `.github/workflows/ci.yml` — test + dogfood (`--fail-on error .`; defaults exclude testdata) |
| R-023 | Close design §15 as decisions in ARCHITECTURE | done | [DECISIONS] D-015-01…D-015-10 |

## [SOMEDAY] Nice later

| ID | Item | Status | Notes |
|---|---|---|---|
| R-030 | Real parsers for Python/JS (replace regex IR where false negatives hurt) | todo | |
| R-031 | Config schema + `codeintegrity init` writing `.codeintegrity.yml` | todo | |
| R-032 | SARIF/HTML polish + README “dogfood this repo” as primary quickstart | todo | |

## Suggested sequence

1. R-001 → R-002 → R-020 (stability + quiet dogfood)
2. R-003 → R-021 → R-022 (CLI honesty + CI)
3. R-023 done → implement D-015 defaults: R-013 → R-017 → R-014 → R-015 → R-016
4. R-004 (module rename) when ready for a breaking import-path change
5. R-011 → R-012 → R-010 (signal quality, then non-Go check)
6. R-030+ as needed
