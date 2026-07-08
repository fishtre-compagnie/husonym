---
title: AI-Assisted Anonymization Configuration
description: Understand what data the AI assistance processes, where it is processed, and how to disable it
id: ai-assisted-anonymization
hide_title: false
slug: /guides/ai-assisted-anonymization
# cSpell:words Qwen quantized piidetect IBAN PESEL rescanned GGUF
---

## Introduction

Husonym can assist you in configuring your anonymization rules: a PII detection
job scans the tables of the source connection, then Husonym **suggests** a
suitable transformer per column (with a category, a confidence level and a
justification). Nothing is ever applied automatically: every suggestion is
reviewed, accepted or rejected by a user.

This page is written with DPOs and compliance teams in mind: it describes
precisely what data is processed, where it is processed, and how to disable
each stage.

## How it works

Detection runs as a cascade, orchestrated by the `piidetect` workflow:

1. **Stage 1 — metadata (always local)**: regular expressions and a
   multilingual dictionary (fr, en, de, es, it, nl, pt, pl) applied to **column
   names and types** only. Columns recognized with a confidence at or above the
   threshold (`PII_DETECT_CONFIDENCE_THRESHOLD`, default `0.8`) leave the
   pipeline.
2. **Stage 2 — LLM (optional)**: only the columns that remain ambiguous are
   submitted to a language model through an OpenAI-compatible API. What the
   model sees depends on the **sampling mode** (see below).
3. **Recommendation**: the backend combines the detection report with the
   transformer catalog and the schema constraints (data types, primary and
   foreign keys) to suggest a configuration per column. This step is
   deterministic, with no AI involved.
4. **Human review**: the suggestions panel in the mapping editor lets you
   accept or reject each proposal, line by line or above a confidence threshold
   of your choosing.

## What data is processed

The sampling mode of the `piidetect` job determines what the LLM sees:

| Mode | What the LLM sees | GDPR status |
| --- | --- | --- |
| **Disabled** (default) | Column names and types only | No data is processed |
| **Profile** (default once sampling is enabled) | One aggregated shape profile per column, computed locally in the worker: patterns (`9`=digit, `A`/`a`=letter), lengths, character set, cardinality, verdicts of local format detectors (IBAN, French NIR, PESEL…) | Anonymous by construction — no value ever leaves the worker |
| **Raw** (explicit opt-in) | Up to 5 rows of raw values per table | Personal data processing: reserve it for the local mode, or cover it with a DPA with the API provider |

Regardless of the configuration:

- unchanged tables are never rescanned (incremental scanning based on a
  fingerprint);
- sampled values are **never written to logs**;
- a token budget caps the size of every request sent to the LLM.

## Where the data is processed

The LLM client is configured through environment variables on the worker and
supports two modes:

- **Local mode (recommended)**: a
  [llama.cpp](https://github.com/ggml-org/llama.cpp) sidecar serves a GGUF
  model on the infrastructure where Husonym is deployed. No data leaves your
  infrastructure; no AI-provider DPA is required.
- **External API mode (opt-in)**: any OpenAI-compatible endpoint. Column names,
  types and shape profiles are then sent to the chosen provider — and raw
  values only if the **Raw** sampling mode is enabled. This mode belongs in
  your record of processing activities and, where applicable, requires a DPA
  with the provider.

### Worker configuration

| Variable | Purpose |
| --- | --- |
| `LLM_BASE_URL` | URL of the OpenAI-compatible API (e.g. `http://husonym-llm:8080/v1`). When unset (along with `LLM_API_KEY`/`OPENAI_API_KEY`): LLM stage disabled |
| `LLM_API_KEY` | API key (optional in local mode; `OPENAI_API_KEY` is still honored) |
| `LLM_MODEL` | Name of the model to invoke |
| `PII_DETECT_CONFIDENCE_THRESHOLD` | Stage-1 confidence threshold (default `0.8`) |

### Deploying the local sidecar

With Docker Compose, an overlay is provided:

```bash
# Download a GGUF model (Qwen3-4B-Instruct Q4_K_M by default, with SHA256 verification)
./scripts/fetch-llm-model.sh

docker compose -f compose.yml -f compose/compose-llm.yml up -d
```

With Helm, the worker chart exposes an optional block:

```yaml
llm:
  enabled: true
  baseUrl: http://husonym-llm:8080/v1
  model: qwen3-4b-instruct
```

The model is a plain file mounted as a volume: swapping it requires no code
change. Plan for roughly 4 CPU / 6 Gi for a quantized 4B model; execution is
asynchronous (Temporal), so latency does not impact the UI.

## How to disable it

Each stage can be disabled independently:

- **LLM stage**: leave `LLM_BASE_URL` unset (and no API key). Stage 1
  (regex + dictionary, fully local) keeps working and the workflow logs the
  LLM stage as disabled.
- **Sampling**: keep the **Disabled** mode in the `piidetect` job
  configuration — only column names and types are then processed.
- **Entire feature**: mapping suggestions are gated by the Enterprise license;
  without a valid license the recommendation RPC is inactive and no LLM scan
  is registered.

## Custom transformer proposals

When a sensitive column matches no transformer in the catalog, the
recommendation may include a **JavaScript transformer draft** generated by the
LLM. Guarantees:

- generated code is always validated (compiled) before being shown; an invalid
  draft is replaced by a generic suggestion;
- **human review is blocking**: the code opens pre-filled in the usual creation
  form, where it is read, tested against example values, then saved — never
  created or assigned automatically;
- the UI explicitly flags the code as AI-generated;
- the code runs in the same sandbox as any hand-written JavaScript transformer:
  no new execution surface.

## Limitations and responsibilities

The AI assistance **helps configure** anonymization; it guarantees neither
exhaustive detection nor regulatory compliance. False negatives remain
possible: human review of the mappings — in particular of columns left as
`Passthrough`, which are flagged with a badge — is part of the process. The
qualification of processing activities and their registration remain the
responsibility of the data controller.
