# codeintegrity Architecture

| | |
|---|---|
| **Product** | codeintegrity (multi-language code integrity assessor) |
| **Version** | 0.4.0 |
| **Date** | 2026-11-04 |

## [OVERVIEW]

Deterministic static integrity analysis across Go, Python, JavaScript/TypeScript, and CSS.
A top-level **assessor** detects languages and dispatches per-language drivers under `lang/*`.
One CLI (`codeintegrity`) covers multi-language crawl plus the deep Go surface (check, flow, schema, suppress).

Painful workflow: mixed repos need several tools and report formats.
Slice 1: assessor + Go SSA/RTA engine + Python/JS/CSS deep engines, one merged report.

## [CONVENTIONS]

- Module path: `gointegrity` (Go module name; CLI binary: `codeintegrity`)
- Shared report schema: `gointegrity/report/v1` with `language` on findings
- Drivers implement `lang.Driver` (`Detect`, `Crawl`, `Rules`)
- Crawl routing: package patterns (`./...`) or Go-only flags → deep Go engine; otherwise multi-language assessor

## [BOUNDARIES]

| Package | Owns |
|---|---|
| `assessor/` | detection, dispatch, merge, exit policy inputs |
| `lang/` | Driver interface |
| `lang/go` | Go SSA/RTA crawl (wraps loader/callgraph/rules/flow) |
| `lang/python` | Python driver (Detect/Crawl) |
| `lang/python/engine` | Deep Python IR + DEAD/STUB/COH/SUP/FLOW + types |
| `lang/js` | JS/TS driver |
| `lang/js/engine` | Deep JS/TS IR + DEAD/STUB/COH/SUP/FLOW + types |
| `lang/css` | CSS driver |
| `lang/css/engine` | Deep CSS DEAD/COH/placeholder/suppress |
| `loader/`, `callgraph/`, `rules/`, `flow/`, `check/` | Go analysis core |
| `report/` | text/JSON/SARIF rendering |
| `schemas/` | check-request and report JSON schemas |
| `cmd/codeintegrity` | sole CLI (multi-lang crawl + Go check/rules/schema/suppress/flow) |

Drivers must not import sibling language packages. Assessor must not parse source ASTs.
