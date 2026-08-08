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
go run ./internal/ee/license/cmd/husonym-license issue \
  --to "Acme Co." --customer-id acme-001 --days 365 \
  --max-jobs 20 --connection-types postgres,mysql \
  --note "contract 2026-A"
```

It prints the `EE_LICENSE` value to give the customer, and appends an entry to the
registry. Add `--dry-run` to validate without recording anything.

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
go run ./internal/ee/license/cmd/husonym-license expiring --within 45

# Everything issued, with its current lifecycle state
go run ./internal/ee/license/cmd/husonym-license list

# One license, including the value to re-send a customer who lost theirs
go run ./internal/ee/license/cmd/husonym-license show <license-id>

# Check a license through exactly the path the product uses at startup
go run ./internal/ee/license/cmd/husonym-license verify "$EE_LICENSE"
```

Re-sending from `show` is preferable to issuing a replacement: two live licenses for one
contract makes the registry ambiguous about what is actually in the field.

### Cloud licenses

`./scripts/gen-cust-license.sh` remains for the **cloud** license, which is signed with a
different key (`husonym_cloud_ca.key`) and verified by `internal/ee/cloud-license`.
`husonym-license` only mints EE licenses.
