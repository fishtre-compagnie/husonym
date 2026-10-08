# Control plane

A small service that receives the sealed daily reports posted by Husonym instances, checks each
one against the registry of issued licenses, and stores it in its own PostgreSQL database.

It is one binary, `husonym-controlplane`, and it imports nothing from the backend, the worker or
the CLI. It reuses `internal/license` (the keyring and the registry) and `internal/telemetry` (the
schema of a report and its seal).

## Commands

| Command                                                  | What it does                                                        |
| -------------------------------------------------------- | ------------------------------------------------------------------- |
| `husonym-controlplane migrate up`                        | Applies the pending migrations of the database (they are embedded). |
| `husonym-controlplane import-registry --registry <file>` | Loads the registry of issued licenses and prints three counts.      |
| `husonym-controlplane serve public`                      | Applies the migrations, then serves the public API.                 |
| `husonym-controlplane serve backoffice`                  | Serves the operator console. It applies no migration.               |

`import-registry` verifies every entry against the embedded keyring. An entry whose key does not
verify, whose id is not the one inside its key, or whose key names no customer is skipped and
counted; the others are imported, and the command exits non-zero. Reports that were waiting for a
license that has just been imported are checked in the same run. Should that last step fail, the
counts are printed all the same, the command exits non-zero, and the server takes it up again
within the hour.

## Environment

| Variable                    | Meaning                                           |
| --------------------------- | ------------------------------------------------- |
| `CONTROLPLANE_DATABASE_URL` | PostgreSQL connection string (required).          |
| `CONTROLPLANE_LISTEN_ADDR`  | Listen address of the server, `:8080` by default. |
| `CONTROLPLANE_METRICS_ADDR` | Listen address of the metrics of `serve public`, `:9090` by default. |
| `CONTROLPLANE_BACKOFFICE_HOST` | Host name the console answers under, a bare name (`serve backoffice`, required). |
| `CONTROLPLANE_ACCESS_TEAM_DOMAIN` | Domain of the Cloudflare Access team, a bare name (`serve backoffice`, required). |
| `CONTROLPLANE_ACCESS_AUD` | Audience tag of the Access application of the console (`serve backoffice`, required). |
| `CONTROLPLANE_SIGNING_KEY_FILE` | Path of the PEM Ed25519 private key the console issues licenses with (`serve backoffice` only, optional). |

The tables live in the `controlplane` schema. The table in which golang-migrate keeps the version
of the schema, `schema_migrations`, sits in `public`.

## Public API

- `POST /v1/usage-reports`: the body is the JSON document of a report, at most 128 KiB, sent with
  `Content-Type: application/json`. Two headers carry the seal and the fingerprint of the license
  key, `Husonym-Seal` and `Husonym-Key-Fingerprint`, each 64 lowercase hexadecimal characters.
- `GET /healthz`: answers 200.

Replies have no body.

| Status | When                                                                                           |
| ------ | ---------------------------------------------------------------------------------------------- |
| 204    | The report is stored, kept pending, already there, or in conflict with the one already stored. |
| 400    | The content type or a header is missing or malformed, or the report is refused: see below.     |
| 404    | Any other path.                                                                                |
| 405    | Another method than `POST` on the reports, or than `GET` on the health check.                  |
| 413    | The body is larger than the cap; a declared length over it is answered before it is read.      |
| 503    | A cap on the pending reports is reached, or the service failed. The sender tries again later.  |

A report is refused with 400 when:

- it is not a document of the schema;
- its day is more than one day ahead of the day it is received, or more than 60 days back;
- its seal is not the one of the document under the license of the fingerprint;
- the fingerprint in the header is not the one in the document;
- the license id it states is not the one of the license it is sealed under;
- its license was already seen on 50 instances in the last 60 days, and this instance is not one
  of them.

A stored report and a pending one answer the same. The other answers differ: a wrong seal, for
one, is only refused under a license that is known.

A report whose license is not in the registry yet is kept pending for 45 days (at most 100 per
fingerprint and 10000 in all). It is checked once the license is imported, as it would have been
on the day it was received, then stored or discarded. The first report stored for a license, an
instance and a day stays; a different one for the same three is counted as a conflict and dropped.

## Operator console

`serve backoffice` serves pages, rendered on the server, that show what the database holds, and
the few acts of the operator. A page is a `GET` and changes nothing; an act is a `POST`.

| Path                                                     | What it shows                                                   |
| -------------------------------------------------------- | --------------------------------------------------------------- |
| `/`                                                      | What needs attention, in five lists: see below.                 |
| `/customers`                                             | The customers.                                                  |
| `/customers/{id}`                                        | A customer, its licenses and its instances.                     |
| `/licenses/{id}`                                         | A license: what its key carries, its instances, refused seals.  |
| `/licenses/{license}/instances/{instance}`               | An instance and its reports, the sources against the source cap. |
| `/licenses/{license}/instances/{instance}/reports/{day}` | A report: its document as received, indented.                   |
| `/pending`                                               | The pending reports, by key fingerprint.                        |
| `/journal`                                               | The 200 latest acts of the operators, the newest first.         |
| `/static/console.css`                                    | The stylesheet. The pages load nothing else.                    |

Instants are shown in UTC.

### The acts of the operator

| Request                                | What it does                                                                |
| -------------------------------------- | --------------------------------------------------------------------------- |
| `GET /customers/new`, `POST /customers` | Records a customer: an external id, a name, a note.                        |
| `GET /customers/{id}/edit`, `POST /customers/{id}` | Changes the name and the note of a customer. Its external id never changes: the keys issued carry it. |
| `GET /customers/{id}/licenses/new`     | The form of a license for the customer; with `?trial=1`, prefilled as a trial of 30 days with every feature. |
| `GET /licenses/{id}/renew`             | The same form, prefilled from the license it succeeds.                      |
| `POST /licenses/confirm`               | Checks the form. With problems, the form again with them; without, every line of the key to be signed, to confirm. |
| `POST /licenses`                       | Signs the license, records it, and shows its key.                           |
| `POST /licenses/{id}/key`              | Shows the key of a license again.                                           |

A license cannot be deleted or changed once issued: no route does it. A license has one successor
at most: the page of a license that has one does not offer to renew it, and a renewal is refused
with 409 when its form is sent, before any confirmation, if the license to renew is not recorded,
belongs to another customer or already has a successor. The store refuses it again when the
license is recorded, also of two renewals sent at once. A license of any id is renewed, whether
the console issued it or not; the one that succeeds it bears an id the console draws. A license
whose key carries a limit the form has no field for is not renewed here: `husonym-license` issues
its successor.

A key carries the external id of its customer as it is recorded. A customer recorded from the
console has its external id without the space around it. To a customer whose recorded external id
begins or ends with a space, the console issues no license: the page says why in place of the
form, and nothing is trimmed when a license is issued.

The key of a license is shown on two pages only, both the answer to a `POST`: right after the
issue, and when it is asked for again. It is in no other page, in no URL and in no log.

The id of a license is drawn when its form is sent, and the confirmation carries it. A
confirmation sent twice issues one license: the second answer signs nothing, shows no key, and
leads to the page of that license, which has its button to show the key again. A confirmation
under the id of a license that says anything else than the draft is refused with 409, and nothing
is signed.

Every act is journaled in the transaction of its write, under the email of the operator (`local`
without the gates): `customer_created`, `customer_updated`, `license_issued`, `license_renewed`,
`license_key_shown`. A line holds the instant, the operator, the customer, the license, and a
short detail: the external id and the name of a customer recorded, its name before and after a
change, the plan, the expiry, the telemetry mode and the license succeeded of a license issued.
It never holds a key.

A `POST` that the browser was made to send by another origin is refused with 403 and does
nothing: one whose `Sec-Fetch-Site` is neither `same-origin` nor `none`, or, without that header,
whose `Origin` is not the host the request was sent to (`http.CrossOriginProtection`). The body
of a `POST` is a form of at most 64 KiB; a larger one is answered 413, one that cannot be read 400.

After an issue, the reports that were pending under the fingerprint of the new key are checked
at once. Should that fail, the issue stands and the public server takes it up within the hour.

### The signing key

`CONTROLPLANE_SIGNING_KEY_FILE` names a file holding the private key, as written by
`openssl genpkey -algorithm ed25519`. It is read once, when the command starts. Its public key
must be one of the keyring the product verifies licenses against. The command refuses to start
when the variable is set and the file is missing, unreadable, larger than 16 KiB, not such a key,
or a key outside the keyring; the error names the variable and the reason, never the path nor
anything of the file. The key is never logged, shown or returned.

Without the variable the console issues nothing: the customers are still recorded and changed and
a key can still be shown again, the pages say issuing is not configured, and the two forms of a
license and the two `POST`s that follow them answer the not found page. `serve public` does not
read the variable.

The five lists of `/` are the silent instances, the expiring licenses, the old pending reports,
the seal rejections and the shared licenses, each as the gauge of the same name counts it (see
Metrics). One differs: the list of the seal rejections covers the last 7 days, the current one
included, where its gauge counts the current UTC day only.

A license id or an instance id that the database cannot hold (not valid UTF-8, or with a zero
byte) is answered the not found page. One that is `.` or `..` is shown as text, without a link.

### The gates

Every request passes two gates, in this order, before a page is rendered:

1. The host. A request whose `Host` is not `CONTROLPLANE_BACKOFFICE_HOST` (its port set aside) is
   answered 404 with no body, whatever it carries.
2. Cloudflare Access. The request must carry, in `Cf-Access-Jwt-Assertion`, a token signed with
   RS256 by a key of the team (`https://<team domain>/cdn-cgi/access/certs`), issued by
   `https://<team domain>`, naming `CONTROLPLANE_ACCESS_AUD` among its audiences, within its
   validity, and carrying an email. Any other request is answered 401 with no body. A token issued
   for another application of the same team does not pass.

`GET /healthz` answers 200 outside both gates, without reading the database, and is the only such
path. The server serves no report intake, and the public server serves no page of the console.

The command refuses to start when one of its variables is missing, when the host or the team
domain is not a bare host name, when the keys of the team cannot be fetched, or when the database
cannot be reached. Request headers are capped at 64 KiB.

Every answer of the server carries `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`,
`Referrer-Policy: no-referrer` and a content security policy that allows the stylesheet, forms
sent to the console itself (`form-action 'self'`) and nothing else: the pages, the 404 of the host
gate, the 401 of the Access gate and the health check alike.

### On one's own machine

`serve backoffice --insecure-no-access` skips both gates and does not read their three variables.
Every page then says so in a banner and shows the operator as `local`. The flag is refused unless
`CONTROLPLANE_LISTEN_ADDR` is a loopback address (`127.0.0.1:8080`, `[::1]:8080`,
`localhost:8080`); the default, `:8080`, listens on every interface and is refused.

In that mode the console answers only local names: a request whose `Host`, its port set aside, is
not `localhost` or a loopback address (`127.0.0.0/8`, `::1`) is answered 404 with no body. A page
open in the same browser cannot then read the console by making its own name resolve to this
machine. `GET /healthz` stays outside that check too.

## What is stored

- For each license: its encoded key and what the key carries, the customer the key names (id and
  name), and from the registry the note and the fingerprint of the signing key.
- For each instance of a license: when its first and its last report were received, the day of its
  last report, the version of the product, and the kind of installation when the report tells it.
- For each report: its document exactly as received, its seal, when it was received, and how many
  different documents were sent again for the same license, instance and day, with the time of the
  last one. The same document sent again is not counted.
- For each license and day: how many reports were refused for their seal, and when the last was.
- The pending reports, as received: fingerprint, instance, day, document, seal and time of
  reception.
- For each customer recorded from the console: its external id, its name and a note.
- For each license issued from the console: what is stored of any license, the note typed with
  it, and the license it succeeds.
- The journal of the operators: see "The acts of the operator".

Nothing else about the caller is stored or logged: no address, no header other than the seal and
the fingerprint, and those two are stored, never logged.

## Logs

The line of a request is a path, a status and a fixed word for the outcome; a path that is not one
of the service is written `-`. When the service itself fails, the line carries the text of its own
error as well. The hourly maintenance writes one line with three counts (stored, discarded,
purged), or the text of its error. What `net/http` would log by itself is dropped.

The backoffice writes one line per request to the console: the email of the operator, the method,
the pattern of the route (`GET /licenses/{id}`, never the path as written; `-` when no route
matched) and the status. No value of a form is logged. A read or a write that fails adds a line
with the text of our own error; a refusal of the signer adds a line in fixed words. A request
refused by the Access gate is one line in fixed words, and so is a panic outside the pages, which
is answered 500; a request refused for its host, and the health check, are not logged.

A read of the gauges that fails is one line with the text of our own error. A panic of the public
server is one line in fixed words.

## Metrics

`serve public` serves `GET /metrics` on its own address, `CONTROLPLANE_METRICS_ADDR`; the report
address serves none. Both listeners stop together, and `serve public` fails if either one cannot
be bound. The gauges are read from the database at most once every 60 seconds while the reads
succeed; a read that fails is counted and logged, and the next scrape tries again.

- `husonym_controlplane_usage_reports_total{outcome}`: report requests received, by the fixed
  word of the log line. Every word has its series from the start, at 0; a request that ended in a
  panic is counted as `panicked`.
- `husonym_controlplane_silent_instances`: instances whose license is in force and whose telemetry
  is online, with a last report more than 3 days and no more than 30 days ago.
- `husonym_controlplane_expiring_licenses`: licenses expiring within 30 days, or in grace, that no
  other license succeeds.
- `husonym_controlplane_old_pending_reports`: pending reports received more than 24 hours ago.
- `husonym_controlplane_seal_rejections_today`: reports refused for their seal on the current UTC
  day (the page lists the last 7 days).
- `husonym_controlplane_shared_licenses`: licenses seen within 30 days on more than one instance.
- `husonym_controlplane_attention_read_failures_total`: reads of the five gauges that failed; the
  gauges are left out of a scrape whose read failed.

## Tests

The integration tests start a PostgreSQL container and need Docker:

```sh
INTEGRATION_TESTS_ENABLED=1 go test ./controlplane/...
```

The generated code in `controlplane/gen/` is produced with sqlc from the queries in
`controlplane/sql/queries` and from the schema in `controlplane/migrations/sql`.

## Image

`docker build -f docker/Dockerfile.controlplane .` builds the image. Its entrypoint is
`/husonym-controlplane`, run as the non-root user 65532, and its default command is `serve public`.
The console is the same image run with `serve backoffice`.
