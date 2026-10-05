# Recorded Presidio answers

Both files hold requests sent to a real Presidio, each with the status and the body it answered.
The analyzer's answers pin how positions are counted and what a request finds. The anonymizer's
are a reference: the product does not call the anonymizer, and `reference_test.go` compares what
this package writes with what the anonymizer wrote for the same findings.

Recorded on 2026-10-03 from:

| Service    | Image                                           | Digest                                                                    |
| ---------- | ----------------------------------------------- | ------------------------------------------------------------------------- |
| analyzer   | `mcr.microsoft.com/presidio-analyzer:2.2.362`   | `sha256:286e3fa7f3a7426e775e8564fe1870f1ba8f999d3ab8bbb8cc46a44355d9d6e9` |
| anonymizer | `mcr.microsoft.com/presidio-anonymizer:2.2.362` | `sha256:a10a12a2a613d13cf29d3ad3641e3258444dd8c90403dd644a0a114c472c2483` |

Both run with their default configuration (`docker run -p <port>:3000 <image>`).

| File             | Requests                                                                                                                                                                                                                         |
| ---------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `analyze.json`   | `POST /analyze`: texts in several scripts (accents, emoji, combining characters, CJK, right-to-left); a threshold left out, of 0, of 0.85 and of 0.85 as a 32-bit number; deny lists by language and by list of entities          |
| `anonymize.json` | `POST /anonymize` with hand-written findings: replace, redact, mask and hash in each of their shapes; findings that overlap, contain each other, cover the same characters, touch, or are separated by spaces and other blanks |

The answers of the analyzer keep the entity type, the positions and the score of each finding.

To record them again, start both images and send each `request` of the two files to its service;
then replace the tag and the digests above.
