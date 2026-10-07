# Licensing

What a license key says, what the product does with it, and how to issue one.

This is the internal reference. Customer-facing wording lives in
`docs/docs/deploy/licensing.md`.

## The mechanism

A license key is a JSON payload signed with **Ed25519**, base64-encoded and handed to the
customer. It looks like this:

```
key = base64({ "license": base64(payload), "signature": base64(sig), "kid": "k1" })
```

The verifying public keys are **embedded in the binary** (one `keys/<kid>.pem` per key, via
`go:embed`). The envelope names the key to verify with in `kid`; it sits outside the signed
content, so altering it can only make verification fail. A key without a `kid`, which is
every key issued before kids existed, is verified with `k1` (`license.LegacyKid`).
Verification is entirely offline: no phone-home, no network call, so an air-gapped
deployment works and we collect nothing about customer usage. The consequence is that there
is **no revocation** — a license is valid until it expires.

### Where the key lives

One key is in force per instance, and it is kept in the product's database: the table
`license_keys`. Rows are only ever added, never changed or deleted, so replacing a key is
adding a row and the older ones stay as history. The key in force is the one **issued** most
recently (`issued_at` of the key, not the time it was stored); of two issued at the same
instant, the one stored last.

A key reaches the instance by being offered to one rule, `licensestore.Store.Offer`, which
is the same whichever way the key came:

| The offered key | Outcome |
| --- | --- |
| signature invalid, unknown `kid`, empty or unreadable | refused |
| identical to the key in force | unchanged |
| issued before, or at the same instant as, the key in force | refused |
| issued after the key in force, even if it has already expired | accepted and stored |

An expired key is accepted when it is newer: the rule decides on the issue date alone, and
the lifecycle then says what the key allows. What the rule refuses never carries the key
value in its reason, so it can be shown to the person who pasted it. Two offers made at the
same moment, wherever they are made, are looked at one after the other (an advisory lock
held for the transaction).

There are two ways to offer a key:

- **`EE_LICENSE` and `EE_LICENSE_FILE`** are entry doors, no longer the key in force. Only
  the API reads them. At its start the API offers the content of the file, then the value
  of the variable (with origin `file` and `environment`), after the database migrations
  that create the table. The file is read again once a minute, and offered again whenever
  its content changes, so a renewed license written to the file takes effect without a
  restart. A variable that holds an older key than the one in the database is simply
  refused, and logged: **the newest key wins, not the file**. A key that is refused, a file
  that cannot be read or a database that does not answer is logged and never stops the
  start. When the database did not answer for the variable at the start, its value is
  offered again once a minute until the database has answered once (accepted, unchanged,
  older and invalid are all answers), then no more; a key accepted that way is in force at
  once. Each offer through a door, and each read of the key in force, is bound to ten
  seconds, so a database that accepts the connection and says nothing hangs neither the
  start nor a refresh.
- **The License page** of the web app, and the RPC behind it,
  `UserAccountService.SetSystemLicense`, with origin `interface`. The call is not gated by
  the license itself: it is how an instance without a valid one gets one.

Who may call `SetSystemLicense`: an administrator of an account, the account being the one
named in the request. In practice any signed-in user is an administrator of their own
account, so the check does not single out who may set the license of the instance. That is
acceptable because of the rule above: a key is only taken when we signed it and it is
strictly newer than the one in force, so no caller can widen the license beyond what we
signed or bring an older one back. It gets narrower once an instance belongs to a single
organization.

The API never returns the key value, except to the worker. `GetSystemLicenseKey` is
guarded as the worker's alone (`userdata.WorkerOnly`): with authentication enabled it needs
the worker's API key. What the API tells everyone else (`GetSystemInformation`) is a
description: state, dates, features, limits, how the key was installed.

### How it circulates

The license is a `license.Provider`. It holds the last key that a `license.Loader` gave it:
`Refresh` asks the loader, verifies what comes back and replaces the key in place.

- The API's loader reads the database (`licensestore.Store.Current`).
- The worker's loader asks the API for the key (`licenseloader.FromAPI`, calling
  `UserAccountService.GetSystemLicenseKey` with the worker's API key) and **verifies it
  itself** against its own embedded keys. The API only hands the value over. An
  `EE_LICENSE` or `EE_LICENSE_FILE` variable left in place on the worker is ignored and
  harmless.

The API refreshes once when it starts and starts either way. The worker does not poll
Temporal until its loader has **answered** once (`licenseloader.AwaitFirstAnswer`): "the
instance holds no key" is an answer, and so is a key the worker refuses; an API that does
not answer is not, and the worker asks again every five seconds, each attempt bound to ten
seconds. A worker that took work before that would believe there is no license while the
API, which holds one, starts runs. Both then refresh once a minute in the background
(`RefreshEvery`). A key stored by
another API instance, or through the interface, therefore reaches every process within a
minute; the API that took a key through `SetSystemLicense` refreshes at once. A value that
cannot be loaded or verified is logged, kept as `Problem()`, and never replaces the key
already in place; a loader that has nothing to give does not take the key away either, so a
database or an API that does not answer leaves the last verified key in force. A load cut
short by a shutdown (a cancelled context) is neither logged nor kept; one that runs past its
deadline is a problem like any other.

The job-hooks activity is the one place where a worker without the license would have run
a job "without" a feature: it used to skip the hooks. It now fails
(`the worker has not received the instance's license yet: job hooks were not run`) when the
license answer of the run is "not in force" and the job has an active hook for the timing;
with no active hook it does nothing, as before. The API starts no run under a license that
is not in force, so this only meets a worker whose key differs from the API's.

Only `Refresh` calls the loader. Every read (`IsValid`, `HasFeature`, `Limits`, `State`,
`Describe`…) answers from memory and from the clock, without any I/O: workflow code calls
them from the workflow thread, where a blocking call trips Temporal's deadlock detector.
Because the clock is read on every call, the license is checked per request, not when the
process starts, and an expiry takes effect without a restart or a refresh. What the API
and the worker wire at startup follows configuration only.

## Lifecycle

Four states, derived entirely from `expires_at` plus the grace period. Nothing is stored:
the same license yields the same state on any instance at any moment.

| State | When | Paid features |
| --- | --- | --- |
| `valid` | more than 30 days from expiry | work |
| `expiring` | within 30 days of expiry | work, with a warning banner |
| `grace` | past expiry, within `grace_days` (default 14) | **still work**, with a blocking banner |
| `frozen` | past expiry + grace | stop |
| `none` | no key in force: the database holds none, or the process could not verify it | never worked |

**`IsValid()` means "may use paid features", not "is before the expiry date".** The two
diverge during grace, and that is deliberate: every caller gating on `IsValid()` inherits
the grace behavior without knowing the lifecycle exists. Use `State()` when the
distinction matters — banners, logs, diagnostics.

## What a license gates

**Requires a valid license** (`JobService`):

| | |
| --- | --- |
| `CreateJob` | `UpdateJobSourceConnection` |
| `CreateJobRun` | `SetJobSourceSqlConnectionSubsets` |
| `CreateJobDestinationConnections` | `UpdateJobDestinationConnection` |
| `UpdateJobSchedule` | `SetJobWorkflowOptions` |
| `PauseJob` — **resume only** | `SetJobSyncOptions` |
| `ApplyMappingChanges` | |

Plus, outside `JobService`: creating or modifying a job or account hook and turning one
back on, creating or modifying S3 and GCS connections, initializing the schema of a SQL
Server destination. The bulk anonymization call and the PII text transformer need the
`pii_text` feature, which also needs a valid license (see below).

**Deliberately not gated** — this half matters as much:

- every `Get*`, except `GetJobRunLogsStream`, which the `run_logs` feature closes
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
runs freeze alike. When the request names a job, it also asks the job gate whether the job
uses a feature the license does not include, and answers `is_valid: false` with the reason
if so.

If you add another entry point that starts work, gate it or make sure it passes through
that check.

## Features

A key may say which optional capabilities it includes. There are thirteen
(`license.AllFeatures`); each is gated where the table says, always together with a valid
license: a feature is never granted by a license that is not in force.

| Feature | Closed at configuration | At run time |
| --- | --- | --- |
| `job_hooks` | creating or modifying a job hook, turning one on | a job with an enabled hook does not start |
| `account_hooks` | creating or modifying an account hook, turning one on | a run that starts without it announces no event to the account hooks |
| `pii_text` | PII text in a job's mappings; a user-defined transformer that stores it; the bulk anonymization call; the column preview of a PII text; the catalog stops offering it | a job that maps it does not start; the call that rewrites each value is refused |
| `pii_detection` | creating or changing a PII detection job; the PII detection call on a connection | a job of that type does not start; the workflow of a run that a schedule started fails |
| `custom_transformers` | creating or modifying a user-defined transformer; trying a JavaScript rule; JavaScript or a user-defined transformer in a job's mappings; an anonymization call (`AnonymizeSingle`, `AnonymizeMany`) or a column preview that carries one, at top level, as a default transformer or among the anonymizers of a PII text | a job that maps one does not start |
| `subsetting` | setting a WHERE clause on a table of the source (clearing them stays possible) | a job with a WHERE clause does not start |
| `scheduling` | setting a cron schedule, resuming a paused schedule | none: see the known limits |
| `mapping_review` | reviewing and applying mapping changes, setting transformers on them | none: the reconciliation a run does is not review |
| `api_keys` | creating and regenerating an API key | none: existing keys keep authenticating |
| `mcp` | none: the gate is in the CLI's MCP server | tool calls of the MCP server are refused |
| `rbac` | giving a member a role other than administrator | none: existing roles keep applying |
| `sso` | declaring, or trying, the OIDC provider of an account that has none yet | none: a declared provider keeps signing in, and can be replaced and tried whatever the license |
| `run_logs` | none | the logs of a run are not served |

The semantics of the list in a key (`Key.HasFeature`):

- A key that carries **no list** allows every feature. So does a list that holds `*`, the
  ones a later version adds included. Every key issued before the list existed carries none.
- A key that carries an **explicit list** allows exactly those features. An empty list
  allows no optional feature. A feature that a later version adds is not in an explicit
  list, so it is closed for that key.
- A name in a key that this version does not know is ignored.

### What a closed feature does

- It cannot be configured: the API answers `permission_denied` with
  `this license does not include <feature>`. The message names the feature, never a plan,
  and never the key. The web app shows the actions of the feature disabled.
- A job that **uses** it does not start. A job uses a feature when it has an enabled hook
  (`job_hooks`), maps PII text (`pii_text`), maps a custom transformer (`custom_transformers`)
  including JavaScript or a user-defined transformer nested inside the anonymizers of a PII
  text, has a WHERE clause (`subsetting`), or is a PII detection job (`pii_detection`). The
  refusal names every missing feature: `this job uses features the license does not
  include: job_hooks, subsetting`. It is made when a job is created or changed, when a run
  is requested (`CreateJobRun`) and when a run is about to start (`IsAccountStatusValid`
  with the job id, which also holds a scheduled run). The job starts whole or not at all.
- **Reads, stops, deletes and turning off always work**, with the one exception of the
  run logs below. A closed feature never refuses anything else that only reads, cancels,
  terminates or removes, as with an expired license.
  Deleting a hook or an API key, pausing a schedule, turning a hook off and deleting a job
  that uses the feature are all open.

The exception is `rbac`, `sso` and `api_keys`: they only forbid **changes**. Roles already
assigned, an OIDC provider already declared and API keys that exist keep working, since
closing them would lock out the people who administer the instance. `sso` goes one step
further: the gate is on **declaring** a provider, not on keeping one working.
`SetAccountSetting` is the only writer of the provider and nothing removes one, so an
account that already holds a provider replaces it and tries it (`TestAccountSetting`)
whatever the license says, in force or not, feature or not: an identity provider that
changes its issuer or its client id must not leave an account unable to repair its sign-in.
Only an account with no provider stored is asked for `sso`. `run_logs` closes a
read instead: the logs are not served, while the run, its status and its events stay
readable. Like every feature it goes through `EnforceFeature`, which asks for a license in
force first, so run logs are no longer served once the license is frozen or absent: this
is the one read that the lapse of a license closes, and a change from when only
configuration was gated.

Connectors are not features. What a license allows in connections is
`limits.allowed_connection_types`, below.

### Known limits

Said plainly, so nobody relies on more than is there:

- **`scheduling` is gated at configuration only.** Setting a cron and resuming a schedule
  are refused; a schedule that already runs keeps firing. A manual run and a scheduled run
  are the same Temporal action, so the start of a run cannot tell them apart.
- **`mcp` is gated in the CLI's MCP server**, not in the API: the server has no license of
  its own and asks the API (`GetSystemInformation`, kept for a minute) before each tool
  call.
- **PII text reached through JavaScript code is not detected at start.** The gate reads the
  mappings, not the code. The call to the API that rewrites a value fails during the run,
  and the run fails with it.
- **`mapping_review` gates review and apply**, not the reconciliation that a run does of
  the mappings with what it read.
- **With authentication disabled**, anyone who reaches the API can install a key and read
  the signed key: every worker-only procedure is open on such an instance, and so is this
  one.

### Where the gates are

A feature is closed in one place per entry point, never by a `Get*`:
`User.EnforceFeature` (which refuses `this license does not include <feature>` and asks for
a valid license first) in the services, `hooks` for the hook procedures, `licensegate`
for what a job uses, and the worker for the two reads below. The worker reads the license
only through `workflow_shared.LicenseIsValid` and `LicenseAllows`, which record the answer.

## Usage limits

Caps live **inside the signed payload**: a limit the customer can edit is not a limit.

```json
{
  "limits": {
    "max_jobs": 20,
    "max_connections": 10,
    "max_sources": 5,
    "allowed_connection_types": ["postgres", "mysql"]
  }
}
```

Enforced in `CreateJob` (`max_jobs`) and `CreateConnection` (`max_connections`, types);
`max_sources` has its own section below.
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

## Sources and the cap

`limits.max_sources` caps the number of **sources** of the instance. A source is what a
synchronization job reads, counted by engine:

- **PostgreSQL, SQL Server and DynamoDB**: a connection used as the source of a
  synchronization job.
- **MySQL and MongoDB**: a connection and a database, the databases being the distinct
  schemas of the job's mappings (for MongoDB, the database names). One connection read
  through two databases counts twice. A job that maps none counts the connection alone.

A generation job counts for nothing, and neither does a PII detection job. Destinations
never count. Two connections to the same database count twice: the license counts
connections, not hosts. The count is made **across all accounts of the instance**, which is
why `GetLicenseUsage` tells an account its own sources and only the total of the others.

The rule (`JobGate.SourceGuard`): a write is refused when it brings a source the instance
does not have and the total would then exceed the cap. Two writes go
through it: `CreateJob` and `UpdateJobSourceConnection`, which carries the mappings. It is never refused at run time. An instance over a lowered cap keeps running
and can edit its jobs as long as the edit adds no source, so a customer can always come
back under it.

Two writes made at the same moment cannot both take the last room: the guard holds an
advisory lock on the sources of the instance until its transaction ends. While it
holds that lock, a write waits at most five seconds for the row of the job (`lock_timeout`):
past it the write fails and lets the others pass.

`max_jobs` and `max_connections` remain **per account**.

## Issuing a license

Use the tool; do not hand-sign a JSON file. See
[`scripts/gen-license.md`](../../../scripts/gen-license.md) for the full reference.

```console
go run ./internal/license/cmd/husonym-license issue \
  --to "Acme Co." --customer-id acme-001 --days 365 \
  --max-jobs 20 --connection-types postgres,mysql \
  --note "contract 2026-A"
```

It prints the key value and records the issuance in the registry. `--features`, `--max-sources`,
`--plan` and `--telemetry` say what the key carries beyond that (`plan` is a label shown to the
customer and gates nothing; `telemetry` is a field the key carries and the License page
shows); `scripts/gen-license.md` has the details and the rules for each. The tool
**refuses to sign with a key that does not match the one embedded in this build**, printing
both fingerprints — that was the one failure mode guaranteed to be found by a customer
rather than by us.

Add `--dry-run` to validate a request and see the result without recording anything.

## The registry

**`~/.husonym/ee-signing/registry.json`**, next to the signing key. It does not exist until
the first issuance creates it. Override the location with `--registry <path>` on any
command.

```console
# The renewal worklist: expiring soonest first, excluding licenses already frozen
go run ./internal/license/cmd/husonym-license expiring --within 45

# Everything issued, with each licence's current lifecycle state
go run ./internal/license/cmd/husonym-license list

# One licence in full, including the value to re-send a customer who lost theirs
go run ./internal/license/cmd/husonym-license show <license-id>
go run ./internal/license/cmd/husonym-license show <license-id> --json

# Check any licence value through the exact path the product uses
go run ./internal/license/cmd/husonym-license verify "$EE_LICENSE"
```

The file is plain JSON and can be read directly, but `list` additionally shows the state
(`valid`, `expiring`, `grace`, `frozen`), which is not stored — it is derived from the
expiry date each time, so a stale file can never report a stale state.

Prefer re-sending from `show` over issuing a replacement when a customer loses their key:
two live licences for one contract makes the registry ambiguous about what is in the field.

The registry holds customer names and working licences. It is written `0600`, lives outside
this repository, and `.gitignore` carries a backstop in case a copy ever lands here. Back it
up with the signing key — losing it does not break any deployment, but it loses the record
of what was issued and when each license expires.

## Developing locally

**Creating a job now requires a license**, in every compose stack. A stack without one
starts fine and refuses at the first `CreateJob` — the intended consequence of the model,
not a bug.

Issue yourself a long-lived development license once:

```console
infisical run --env=prod -- go run ./internal/license/cmd/husonym-license issue \
  --to "Development" --customer-id dev --days 3650 --note "local dev"
```

Then set `EE_LICENSE=<value>` in `.env.api.local`. Both compose files read that path with
`required: false`, so the license reaches the API container without being committed. The
worker needs nothing: it asks the API, so nothing license-related goes in
`.env.worker.local`. The API takes the key into its database at its start, and the database
then holds it: a key issued earlier than the one the stack already holds is ignored, so to
replace a development key issue a new one. Or paste it into the License page. `compose.yml` did not read `.env.api.local` until
it was fixed alongside this document: the license was documented here long before anything
injected it, and the stack refused with no indication why.

### Tests

Use `testutil.NewFakeEELicense(testutil.WithIsValid())`; `SetValid(false)` makes it lapse
mid-test. The fake allows every feature unless told otherwise, as a key without a list
does:

- `WithFeatures(...)` restricts it to exactly those features (no argument: none);
  `SetFeatures(...)` does the same mid-test and `ClearFeatures()` gives every feature back.
- `WithLimits(...)` exercises caps, `SetLimits(...)` changes them mid-test.

Production code builds one `license.NewProvider(loader, logger)`, refreshes it, and hands it
down as a `license.EEInterface`. The API's loader is `licensestore.Store.Current`, the
worker's is `licenseloader.FromAPI`.

`Test_EveryFeatureHasAGate` (`internal/integration-tests/api`) is what fails when a feature
has no gate: a name added to `license.AllFeatures()` without an entry fails it. For most
features the entry is one call that configures or uses the feature, served under a license
that includes everything and refused, naming the feature, under one that includes
everything else. For `api_keys`, `run_logs` and `mcp`, which that suite cannot reach, the
entry names the test that proves the gate, which must exist. It proves **one representative
call per feature**, not that every entry point of a feature is gated, and it does not cover
the run-time reads of the worker: those have their own tests.

## The signing key

The private key is the one asset that cannot be replaced. Lose it and no customer can ever
be renewed; leak it and anyone can license themselves.

**It is held in Infisical.** The CLI is pinned by aqua (`aqua policy allow aqua/aqua-policy.yaml`
then `aqua i`, see CONTRIBUTING.md); bind the checkout once with `infisical login` and
`infisical init`. Inject the key rather than copying it to disk — the tool reads
`HUSONYM_EE_SIGNING_KEY`, accepting the PEM directly or base64 of it, and prefers it over
`--key`:

```console
infisical run -- go run ./internal/license/cmd/husonym-license issue \
  --to "Acme Co." --customer-id acme-001 --days 365
```

A local copy at `~/.husonym/ee-signing/husonym_ee_ca.key` (`0600`) still works and is what
`--key` defaults to, but treat it as a convenience, not the source of truth. Note where that
path actually lives on a WSL machine: inside the WSL virtual disk, which a
`wsl --unregister` or a disk corruption destroys along with everything else. That is the
reason the authoritative copy sits in a secret manager.

`.gitignore` carries a backstop for `*_ca.key`, `registry.json` and `ee_license`, in case a
copy ever lands in the working tree.

### The key ring and rotation

The binary embeds a ring of public keys, one file per key: `internal/license/keys/<kid>.pem`
(today `k1.pem`), and the kid is the file name without `.pem`. A key names the key that
signed it in the envelope (`kid`); one that names none is verified with `k1`. A key signed
with a kid that a version does not embed is refused with `signed with a key this version
does not know`, and the key in place stays in force.

Rotating is therefore **not** reissuing every live license: replacing the content of
`keys/k1.pem` would stop every existing key from verifying at once, so do not. Add the new
public key beside it instead:

1. Add `keys/<new kid>.pem` and ship a version that embeds it, to the API **and** to the
   workers: each verifies a key itself.
2. Once the deployed versions know it, sign new licenses with the new private key. The
   tool finds the kid of the key it is given in the ring and writes it in the envelope
   (it refuses a key the ring does not hold).
3. Remove the old public key only after the last license it signed has expired, grace
   included. Until then every license signed with it still verifies.

What the code does **not** do yet: the tool signs with the one private key it is given
(`HUSONYM_EE_SIGNING_KEY` or `--key`), and nothing selects among several, nor stores the
private key of each kid. Choosing which one signs is the person issuing. A key signed with
a kid that a customer's version does not know is refused, so mind the order of step 1 and 2.

Tests mint their own throwaway keypairs, so rotation never breaks the suite.

## Workflow replay

The license follows the clock and the key in force changes, so a workflow must not read
it directly: a replay that read it anew could take another branch than the run took. The
datasync and PII detection workflows read it once, at their start, through two helpers of
`worker/pkg/workflows/shared/license.go` that record the answer in the history of the run
(a Temporal side effect behind a version marker):

| Change id | Read | A run started before the change replays |
| --- | --- | --- |
| `license-read-recorded` | `LicenseIsValid`: is the license in force | reading the license directly, as it ran |
| `license-feature-read-recorded` | `LicenseAllows`: does it include a feature | with the answer the code acted on before it asked for the feature (the validity), the license not consulted |

A run keeps the answer it started with to its end, whatever becomes of the license.
Adding a read of the license to a workflow needs a change id of its own, with the old
behavior kept for runs that predate it.

**Rolling the worker back is a hazard.** Once the new worker has started runs, their
history holds markers and recorded values that the previous version does not expect; that
version replays them, fails on the mismatch, and those runs cannot make progress until the
worker is upgraded again. Let the open runs finish, or cancel them, before a worker goes
back. Rolling the API back is safe for the license: the previous version reads its
environment variable again, and the extra table is ignored or removed by the down
migration.

## What this does not protect against

Worth being clear-eyed about, so nobody builds on an illusion:

- A customer receiving **source** can delete the check and rebuild in minutes. The model
  assumes they receive images only.
- A determined party can patch a binary.
