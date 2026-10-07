# Notes on Generating OSS Licenses

## Generate Husonym CA with ED25519

This generates a private key with a password

```console
openssl genpkey -algorithm ed25519 -out husonym_ee_ca.key -aes256
```

## Generate Husonym Pub Key

```console
openssl pkey -in husonym_ee_ca.key -pubout -out husonym_ee_pub.pem
```

## Sign a License File

Signs a file with a provided secret key and generates a signature file

```console
openssl pkeyutl -sign -inkey husonym_ee_ca.key -out license.sig -rawin -in license.json
```

## Verify a License File

Verifies a file with a provided public key and the accompanying signature file

```console
openssl pkeyutl -verify -pubin -inkey husonym_ee_pub.pem -rawin -in license.json -sigfile license.sig
```

## Generate a new EE License

Use `husonym-license`. Do not hand-write a `license.json` and sign it with the shell
script: that path signs whatever you give it, so a mistyped field name produces a
perfectly valid signature over a payload the product then ignores, with nothing to catch
it before the customer does. The tool builds the payload from the same structs the product
verifies, validates it, refuses a key that does not match the build, and records what was
issued.

```console
go run ./internal/license/cmd/husonym-license issue \
  --to "Acme Co." --customer-id acme-001 --days 365 \
  --max-jobs 20 --connection-types postgres,mysql \
  --note "contract 2026-A"
```

It prints the `EE_LICENSE` value to give the customer, and appends an entry to the
registry. Add `--dry-run` to validate without recording anything.

Four options say what the license carries:

- `--plan "Team"` is a label shown to the customer, on the License page. Nothing is decided
  from it: it gates nothing, so the features and limits of a license are set with the options
  below, never with the plan.
- `--features job_hooks,sso` lists the features the license allows, by name; `--features '*'`
  allows every one, including those added later. A name the tool does not know is refused,
  and so is a name listed twice or `*` combined with names. The names are `job_hooks`,
  `account_hooks`, `pii_text`, `pii_detection`, `custom_transformers`, `subsetting`,
  `scheduling`, `mapping_review`, `api_keys`, `mcp`, `rbac`, `sso` and `run_logs`.
  **Omitted and empty are opposites.** Without the option the license carries no list, which
  allows everything, as every license issued before the list existed does. With the option
  given empty (`--features ''`) the license carries an empty list, which allows **no**
  optional feature. A feature added by a later version is not in an explicit list: it is
  closed for that license until a new one names it, whereas `*` and no list include it.
- `--max-sources 5` caps the number of sources of the instance, every account together;
  without it the number is not capped.
- `--telemetry online|offline_report|none` is the mode the license asks of the instance, a
  field the key carries and the API returns (`SystemLicense.telemetry`); without it the
  license says nothing and the field reads `online`. Nothing in the product acts on it or
  reports anything, and the License page does not show it.

Everything, with the instance capped on sources:

```console
go run ./internal/license/cmd/husonym-license issue \
  --to "Acme Co." --customer-id acme-001 --days 365 \
  --features '*' --max-sources 5
```

An explicit list, so that only these features are included:

```console
go run ./internal/license/cmd/husonym-license issue \
  --to "Acme Co." --customer-id acme-002 --days 365 \
  --features job_hooks,account_hooks,scheduling,sso
```

A renewal or a replacement must be **issued after** the key it replaces: an instance takes
a key only when it was issued later than the one it holds, so issue it fresh rather than
reusing an old payload. The tool signs with the one private key it is given, and writes the
kid of that key in the license; the key must be one the build embeds (see the key ring in
`internal/license/README.md`).

By default both the signing key and the registry are read from
`~/.husonym/ee-signing/` (`husonym_ee_ca.key` and `registry.json`); override with `--key`
and `--registry`. **Neither belongs in this repository.** The key is the one asset that
cannot be replaced — lose it and you can no longer renew any customer; leak it and anyone
can license themselves. The registry holds customer names and live licenses, and is
written `0600`.

### Tracking what was issued

Renewals are the revenue, and you cannot chase a renewal you have no record of.

```console
# The renewal worklist, soonest first. Excludes licenses already past grace.
go run ./internal/license/cmd/husonym-license expiring --within 45

# Everything issued, with its current lifecycle state
go run ./internal/license/cmd/husonym-license list

# One license, including the value to re-send a customer who lost theirs
go run ./internal/license/cmd/husonym-license show <license-id>

# Check a license through exactly the path the product uses
go run ./internal/license/cmd/husonym-license verify "$EE_LICENSE"
```

Re-sending from `show` is preferable to issuing a replacement: two live licenses for one
contract makes the registry ambiguous about what is actually in the field.
