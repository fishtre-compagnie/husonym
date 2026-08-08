# Licensing

How Husonym is licensed, what a license gates, and how to issue one.

This is the internal reference. Customer-facing wording lives in
`docs/docs/deploy/licensing.md`.

## The mechanism

A license is a JSON payload signed with **Ed25519**, base64-encoded, handed to the
customer, and set as the `EE_LICENSE` environment variable on the backend and the worker.

The verifying public key is **embedded in the binary** (`husonym_ee_pub.pem`, via
`go:embed`). Verification is entirely offline: no phone-home, no network call, so an
air-gapped deployment works and we collect nothing about customer usage. The consequence
is that there is **no revocation** — a license is valid until it expires, which is why
license terms should be short enough that non-renewal is itself the enforcement.

```
EE_LICENSE = base64({ "license": base64(payload), "signature": base64(sig) })
```

## Lifecycle

Four states, derived entirely from `expires_at` plus the grace period. Nothing is stored:
the same license yields the same state on any instance at any moment.

| State | When | Paid features |
| --- | --- | --- |
| `valid` | more than 30 days from expiry | work |
| `expiring` | within 30 days of expiry | work, with a warning banner |
| `grace` | past expiry, within `grace_days` (default 14) | **still work**, with a blocking banner |
| `frozen` | past expiry + grace | stop |
| `none` | no `EE_LICENSE` set | never worked |

**`IsValid()` means "may use paid features", not "is before the expiry date".** The two
diverge during grace, and that is deliberate: every caller gating on `IsValid()` inherits
the grace behaviour without knowing the lifecycle exists. Use `State()` when the
distinction matters — banners, logs, diagnostics.

Why a grace period rather than a hard stop: a lapsed license is usually a slow invoice, and
cutting a customer's environment off the same day turns an accounting delay into an
incident they blame us for. The commercial lever is enough without it — a sync tool that
cannot sync is already useless.

## What a license gates

Gating widened deliberately: the license used to cover only the EE extras while the product
itself was free, which is the open-core shape inherited from upstream and does not match
selling the tool.

**Requires a valid license** (`JobService`):

| | |
| --- | --- |
| `CreateJob` | `UpdateJobSourceConnection` |
| `CreateJobRun` | `SetJobSourceSqlConnectionSubsets` |
| `CreateJobDestinationConnections` | `UpdateJobDestinationConnection` |
| `UpdateJobSchedule` | `SetJobWorkflowOptions` |
| `PauseJob` — **resume only** | `SetJobSyncOptions` |

Plus the EE extras that were already gated: RBAC, SQL Server, job and account hooks, Loki
run logs, S3 and GCS connections.

**Deliberately not gated** — this half matters as much:

- every `Get*`
- `DeleteJob`, `DeleteJobDestinationConnection`
- `CancelJobRun`, `TerminateJobRun`
- pausing a schedule (only *resuming* is gated)

An account whose license lapsed keeps full read access to its configuration and run
history, and can still stop and clean up. Blocking that would strand a customer with jobs
they can neither run nor quiet. **Do not gate a stop or a delete.**

### Scheduled runs

Gating the API alone would not freeze what matters: Temporal triggers scheduled workflows
directly, never passing through `CreateJobRun`. The choke point is
`IsAccountStatusValid` — the datasync workflow calls it before doing any work and aborts
when it returns false. Self-hosted, it consults the license there, so manual and scheduled
runs freeze alike.

If you add another entry point that starts work, gate it or make sure it passes through
that check.

## Usage limits

Caps live **inside the signed payload**: a limit the customer can edit is not a limit.

```json
{
  "limits": {
    "max_jobs": 20,
    "max_connections": 10,
    "allowed_connection_types": ["postgres", "mysql"]
  }
}
```

Enforced in `CreateJob` (`max_jobs`) and `CreateConnection` (`max_connections`, types).
Reached via `user.LicenseLimits()`, since the license already travels with the user.

Two invariants, both covered by tests — inverting either would lock out paying customers:

- **`nil` means uncapped, never zero.** A license issued before limits existed carries
  none, and must keep working without restriction.
- **An empty `allowed_connection_types` permits everything.** Adding a connector to
  Husonym must never retroactively invalidate a license already in the field.

Connection type names (`postgres`, `mysql`, `mssql`, `mongodb`, `dynamodb`, `aws-s3`,
`gcp-cloud-storage`, `openai`) are a hand-written switch, not derived from generated
protobuf type names, so renaming a generated type cannot silently invalidate licenses.

Counting is done by listing rather than a `COUNT` query — there is no
`CountJobsByAccount` in the generated queries and adding one means regenerating sqlc.
Per-account counts are in the tens. Worth revisiting alongside any other sqlc change.

## Issuing a license

Use the tool; do not hand-sign a JSON file. See
[`scripts/gen-license.md`](../../../scripts/gen-license.md) for the full reference.

```console
go run ./internal/ee/license/cmd/husonym-license issue \
  --to "Acme Co." --customer-id acme-001 --days 365 \
  --max-jobs 20 --connection-types postgres,mysql \
  --note "contract 2026-A"
```

It prints the `EE_LICENSE` value and records the issuance in the registry. The tool
**refuses to sign with a key that does not match the one embedded in this build**, printing
both fingerprints — that was the one failure mode guaranteed to be found by a customer
rather than by us.

Renewal worklist:

```console
go run ./internal/ee/license/cmd/husonym-license expiring --within 45
```

## Developing locally

**Creating a job now requires a license.** `make compose/up` on its own will refuse, which
is the intended consequence of the model and not a bug.

Issue yourself a long-lived development license once and keep it in your local env file:

```console
go run ./internal/ee/license/cmd/husonym-license issue \
  --to "Development" --customer-id dev --days 3650 --note "local dev"
```

Then set `EE_LICENSE=<value>` in `.env.api.local` and `.env.worker.local`.

In tests, use `testutil.NewFakeEELicense(testutil.WithIsValid())` — and
`testutil.WithLimits(...)` to exercise caps. Never wire `license.NewValidLicense()` into
production code: it is unconditionally valid and used to short-circuit the whole cascade.

## The signing key

The private key is the one asset that cannot be replaced. Lose it and no customer can ever
be renewed; leak it and anyone can license themselves. It lives outside this repository
(`~/.husonym/ee-signing/husonym_ee_ca.key` by default), `0600`, with an encrypted offline
backup. `.gitignore` carries a backstop for `*_ca.key`, `registry.json` and `ee_license`.

Rotating it means replacing `husonym_ee_pub.pem`, rebuilding, **and reissuing every live
license** — existing ones stop verifying immediately. There is no dual-key support today;
adding it would be the way to make rotation non-disruptive.

Tests mint their own throwaway keypairs, so rotation never breaks the suite.

## What this does not protect against

Worth being clear-eyed about, so nobody builds on an illusion:

- A customer receiving **source** can delete the check and rebuild in minutes. The model
  assumes they receive images only.
- A determined party can patch a binary. The aim is to make circumvention deliberate and
  demonstrable — which is what makes it contractually actionable.
- The code inherited from upstream is **MIT**, and was published. Those versions stay MIT
  and a fork of them remains legal. The lock applies to future, unpublished versions.

The real protection is the contract plus the distribution model. This mechanism is what
makes the contract enforceable, not a substitute for it.
