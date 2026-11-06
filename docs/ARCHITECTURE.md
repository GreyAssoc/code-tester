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

- Module path (current): `gointegrity` — **decision D-015-09:** rename to match product/repo (`github.com/GreyAssoc/code-tester`); CLI stays `codeintegrity`
- Shared report schema: `gointegrity/report/v1` (schema id may keep historical name until a versioned bump)
- Drivers implement `lang.Driver` (`Detect`, `Crawl`, `Rules`)
- Crawl routing: package patterns (`./...`) or Go-only flags → deep Go engine; otherwise multi-language assessor
- Default fail-on: `warning` for local; CI dogfood uses `--fail-on error`
- Flow context default: `--context=1` (D-015-01)
- No analysis cache in CI or local defaults (D-015-08)

## [DECISIONS]

Closed design §15 open questions (source: `go-code-integrity-design.md` §15). Owner: Architecture.

| ID | Topic | Decision |
|---|---|---|
| **D-015-01** | Context sensitivity | Default **`--context=1`**. Prefer precision; accept cost on large repos. |
| **D-015-02** | Default source/sink/secret lists | **Broad** built-in lists. Prefer catching real bugs over quiet reports; false positives are suppressed/ignored when triaged. |
| **D-015-03** | Dropped-input (FLOW002) | **Strict** by default (severity/confidence and which inputs count). Same triage model as D-015-02. |
| **D-015-04** | Multi GOOS / build tags | **Detect** whether the target tree has Windows-oriented code (e.g. windows build tags / GOOS-specific files). If **no** Windows-oriented surface → **lean** single-config analysis (host/default GOOS). If **yes** → also analyze a Windows build configuration **even when the host is Linux**, so Windows-tagged code is not false-DEAD. |
| **D-015-05** | Field-level dead code | **On.** Report unused struct fields. Requires solid field-use tracking (reflection / `encoding` / tagged fields) before trusting confidence; build that tracking out rather than leaving the rule off. |
| **D-015-06** | Flow policy location | **Both:** standing policies in config (`.gointegrity.yml` / future `.codeintegrity.yml`); overlays in check-request JSON. |
| **D-015-07** | SARIF size / evidence | **Chunk** uploads by witness/path-depth bands (e.g. depths 0–3, 4–7, 8–10, …) so each chunk stays within code-scanning limits while the full scan still covers all evidence. Exact band edges and result caps are implementation details under this policy. |
| **D-015-08** | Analysis cache | **Do not persist** `.gointegrity-cache` in CI or as a default. Always analyze live tree; avoid stale-cache wrong answers. |
| **D-015-09** | Publishing / module path | **Rename** the Go module and fix import paths to align with the published repo/product (target: `github.com/GreyAssoc/code-tester`). CLI name remains `codeintegrity`. |
| **D-015-10** | Target codebase | Product purpose: an **agent** points this tool at **any** other project for deterministic analysis. Self-crawl of `code-tester` is a temporary dogfood/recursive-improvement tactic, not the long-term target. |

### Notes on D-015-04 and D-015-07

- **D-015-04:** “Windows or not” means **codebase shape**, not only the host OS. Host Linux + Windows-tagged packages ⇒ extra Windows config pass.
- **D-015-07:** Chunking is about **emitting/uploading** SARIF in bands so scanners accept payloads; it is not a substitute for a full analysis pass. Implementation may still compute one graph then split serialization.

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

## [ROADMAP]

Prioritized follow-ups live in `docs/ROADMAP.md` (tags R-001…). Architecture owns product decisions that close roadmap items (e.g. R-004 naming, R-023 §15).
