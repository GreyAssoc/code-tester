# codeintegrity

**Deterministic, multi-language code integrity analysis for agents and humans.**

Point this tool at **any** project tree. It detects languages, runs per-language integrity engines, and emits one merged report (text / JSON / SARIF). The goal is a reliable, repeatable assessor that an AI agent (or a CI job) can trust when reviewing or hardening someone else’s codebase.

| | |
|---|---|
| **CLI** | `codeintegrity` |
| **Module** | `github.com/GreyAssoc/code-tester` |
| **Version** | `0.4.0` (single source: `report.ToolVersion` in `report/model.go`) |
| **Repo** | https://github.com/GreyAssoc/code-tester |

Design history (Go-first): [`go-code-integrity-design.md`](go-code-integrity-design.md)  
Architecture & product decisions: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)  
Roadmap: [`docs/ROADMAP.md`](docs/ROADMAP.md)

---

## What this project will ultimately be

When complete, **codeintegrity** is the default integrity sidecar for agentic software work:

1. An agent (or human) names a **target repo** — not this tool’s own source.
2. The assessor **detects** Go, Python, JS/TS, CSS (and later more languages).
3. Language drivers run **deterministic** rules: dead code, placeholders/stubs, coherence, suppressions, sensitive data flow, and (where applicable) light type checks.
4. Results come back as a **single report** agents can act on — fix, suppress, or baseline — without guessing from LLM vibes alone.
5. Go stays the **deepest** engine (SSA / RTA / check-mode / flow policies). Other languages approach the same *rule categories*, with stronger parsers over time.
6. CI and agents share the same CLI, schemas, and fail-on policy. Analysis always runs on the **live** tree (no stale analysis cache by default).

Self-crawling *this* repo was a bootstrap / dogfood tactic. The product mission is **analyze other codebases**.

---

## What has been built (v0.4)

### Core product
- **Single CLI** `codeintegrity` — multi-lang crawl + deep Go surface (`check`, `rules`, `schema`, `suppress`, flow graphs).
- **Assessor** — detect → dispatch → merge findings with a `language` field.
- **Drivers** under `lang/`:
  - **Go** — SSA/RTA call graph, DEAD / STUB / COH / SUP / FLOW, check-request mode, baselines, suppressions, DOT/Mermaid/HTML flow views.
  - **Python / JS-TS** — IR engines with the same rule *categories* (dead, stub, coherence, suppress, flow) plus type heuristics.
  - **CSS** — unused selectors/vars/keyframes, empty rules, coherence, suppressions.
- **Shared report** schema (wire id still `gointegrity/report/v1` for compatibility) with text / JSON / SARIF renderers.

### Hardening & product decisions already landed
| Area | Status |
|---|---|
| Default excludes (`testdata/**`, `node_modules/**`, `vendor/**`, `.git/**`, …) + excluded-path count | Done (R-002) |
| `rules` lists **all** languages (`--lang` filter) | Done (R-003) |
| Module rename to `github.com/GreyAssoc/code-tester` | Done (R-004) |
| FLOW defaults: `--context=1`, broad source/sink lists, strict FLOW002 | Done (R-013) |
| Windows-oriented Go detection → extra `GOOS=windows` pass on Linux | Done (R-014) |
| No persisted analysis cache unless `--cache` | Done (R-017) |
| §15 design questions closed in ARCHITECTURE (D-015-01…10) | Done (R-023) |
| CI: `go test ./...` + dogfood crawl on `main` | Done (R-022) |
| Dogfood of this repo can reach **0 findings** with defaults | Verified |

### Layout

| Package | Role |
|---|---|
| `assessor/` | Language detection, dispatch, merge |
| `lang/` | `Driver` interface |
| `lang/go` | Go SSA/RTA crawl |
| `lang/python` (+ `engine`) | Python integrity engine |
| `lang/js` (+ `engine`) | JS/TS integrity engine |
| `lang/css` (+ `engine`) | CSS integrity engine |
| `loader/`, `callgraph/`, `rules/`, `flow/`, `check/` | Go analysis core |
| `report/`, `schemas/` | Rendering + JSON schemas |
| `cmd/codeintegrity` | Sole CLI |
| `testdata/` | Fixtures (excluded from dogfood by default; keep in tree for tests) |

---

## What is left to build

Tracked in [`docs/ROADMAP.md`](docs/ROADMAP.md). Highlights:

### Next (product depth)
| ID | Item |
|---|---|
| **R-015** | Field-level dead code + solid field-use tracking (json/reflection) |
| **R-016** | Chunked SARIF by witness/path-depth bands (for code-scanning size limits) |
| **R-011** | De-dupe overlapping findings (STUB + COH + DEAD on one symbol) |
| **R-012** | `--mode library\|app` (smarter roots when there is no `main`) |
| **R-010** | Non-Go `check` mode (request / baseline / suppress for Python & JS) |

### Someday
| ID | Item |
|---|---|
| **R-030** | Real parsers for Python/JS (replace regex IR where it hurts) |
| **R-031** | Config schema + `codeintegrity init` → `.codeintegrity.yml` |
| **R-032** | SARIF/HTML polish; README quickstart already points here |

Known residual: occasional SSA skip warning on `internal/poll` with current `x/tools` (recover keeps the crawl alive).

---

## Install

```sh
go install github.com/GreyAssoc/code-tester/cmd/codeintegrity@latest

# from a clone:
git clone https://github.com/GreyAssoc/code-tester.git
cd code-tester
go build -o codeintegrity ./cmd/codeintegrity
```

Requires Go **1.23+**.

---

## Usage

```sh
# Multi-language crawl of any project
./codeintegrity -C /path/to/other/repo
./codeintegrity --lang go,python,js,css --format json -o report.json /path/to/repo
./codeintegrity languages
./codeintegrity rules                  # all languages
./codeintegrity rules --lang python

# Deep Go (package patterns / Go-only flags)
./codeintegrity --no-config --roots main,exported ./...
./codeintegrity crawl --flow --format sarif -o out.sarif ./...

# Go check / policy tooling
./codeintegrity check --request checks.json ./...
./codeintegrity schema report
./codeintegrity suppress --rule FLOW001 --source SRC --sink SNK --reason TEXT --owner WHO
./codeintegrity version
```

Defaults already exclude `testdata/**`, `node_modules/**`, `vendor/**`, `.git/**`, `dist/**`, `build/**`.  
Flow uses `--context=1` when `--flow` is on. Analysis cache is **off** unless you pass `--cache`.

### Dogfood this repo

```sh
./codeintegrity --fail-on error .
```

---

## Rule categories (by language)

| Category | Go | Python | JS/TS | CSS |
|---|---|---|---|---|
| Dead / unused | DEAD001–005 | PYDEAD* | JSDEAD* | CSSDEAD* |
| Placeholders / stubs | STUB001–007 | PYSTUB* | JSSTUB* | CSSEMPTY / CSSSTUB* |
| Coherence | COH001–004 | PYCOH* | JSCOH* | CSSCOH* |
| Suppressions | SUP001 | PYSUP001 | JSSUP001 | CSSSUP001 |
| Sensitive flow | FLOW001–002 | PYFLOW* | JSFLOW* | — |
| Types (extra) | (check mode) | PYTYPE* | JSTYPE* | — |

Run `./codeintegrity rules` for the live catalog.

---

## Tests & CI

```sh
go test ./...
```

GitHub Actions (`.github/workflows/ci.yml`) on every push/PR to `main`:

1. `go test ./... -count=1`
2. Build + dogfood: `./codeintegrity --fail-on error .`

Example consumer workflow: [`examples/github-actions.yml`](examples/github-actions.yml).

---

## Product principles

- **Deterministic over generative** — same tree, same rules, same findings.
- **Prefer recall** — broad sinks/sources and strict dropped-input; triage false positives with suppressions/baselines (D-015-02, D-015-03).
- **Facts, not architecture advice** — report what the code does; don’t prescribe app design.
- **Agent-first target model** — any other project is the customer; this repo is the tool (D-015-10).
- **Live analysis** — no default persisted analysis cache (D-015-08).

---

## License / status

Private product under active development (v0.4). Module and CLI are published for install from this GitHub repository. Schema wire IDs retain the historical `gointegrity/*` names until a deliberate schema version bump.
