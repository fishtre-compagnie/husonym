# Recorded Presidio answers

Every file here is the body of an answer of a real Presidio, saved as it was received. They are
what the simulated Presidio of the tests answers with.

Recorded on 2026-10-03 from:

| Service    | Image                                           | Digest                                                                    |
| ---------- | ----------------------------------------------- | ------------------------------------------------------------------------- |
| analyzer   | `mcr.microsoft.com/presidio-analyzer:2.2.362`   | `sha256:286e3fa7f3a7426e775e8564fe1870f1ba8f999d3ab8bbb8cc46a44355d9d6e9` |
| anonymizer | `mcr.microsoft.com/presidio-anonymizer:2.2.362` | `sha256:a10a12a2a613d13cf29d3ad3641e3258444dd8c90403dd644a0a114c472c2483` |

Both run with their default configuration and listen on port 3000.

The text is
`Très cher Jörg Müller, écrivez à jörg.müller@example.com ou appelez le 212-555-0188.`

| File                                | Request                                                                                                                                                                                                                                  | Status |
| ----------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------ |
| `analyze_ok.json`                   | `POST /analyze` with the text and `"language": "en"`                                                                                                                                                                                     | 200    |
| `analyze_no_language.json`          | `POST /analyze` with `{"text": "x"}`                                                                                                                                                                                                     | 500    |
| `analyze_unsupported_language.json` | `POST /analyze` with `{"text": "x", "language": "zz"}`                                                                                                                                                                                   | 500    |
| `supportedentities_en.json`         | `GET /supportedentities?language=en`                                                                                                                                                                                                     | 200    |
| `anonymize_ok.json`                 | `POST /anonymize` with the text, the `EMAIL_ADDRESS` (33 to 56) and `PHONE_NUMBER` (71 to 83) findings of `analyze_ok.json`, a `replace` operator with `<EMAIL>` for the first and a `mask` of 8 characters with `*` for the second | 200    |
| `anonymize_invalid_operator.json`   | the same with `"anonymizers": {"DEFAULT": {"type": "nope"}}`                                                                                                                                                                             | 422    |
| `anonymize_no_body.json`            | `POST /anonymize` without a body                                                                                                                                                                                                         | 400    |

To record them again, start both images, send the requests with `curl -o <file>`, and replace the
tag and the digests above.
