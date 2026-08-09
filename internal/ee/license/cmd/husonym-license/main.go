// husonym-license mints Enterprise licenses and keeps a record of every one issued.
//
// It replaces scripts/gen-cust-license.sh, which signed whatever JSON you handed it: a
// mistyped field name produced a perfectly signed license the product then ignored, with
// nothing to catch it before the customer did. This tool builds the payload from the same
// structs the product verifies, validates it, and records the result.
//
// The registry is why this matters commercially: renewals are the revenue, and you cannot
// chase a renewal you have no record of.
//
//	go run ./internal/ee/license/cmd/husonym-license issue \
//	  --to "Acme Co." --customer-id acme --days 365 --max-jobs 20
//	go run ./internal/ee/license/cmd/husonym-license expiring --within 45
//
// Both the signing key and the registry live outside the repository. Never commit either.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/ee/license"
)

const usage = `husonym-license — mint Enterprise licenses and track what was issued

Commands:
  issue      Mint a license, record it in the registry, print the EE_LICENSE value
  list       List every issued license with its current lifecycle state
  expiring   List licenses needing renewal, soonest first
  show       Print one license, including the value to hand to the customer
  verify     Check that a license value verifies against this build's embedded key

Run a command with -h for its flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "issue":
		err = runIssue(os.Args[2:])
	case "list":
		err = runList(os.Args[2:], false)
	case "expiring":
		err = runList(os.Args[2:], true)
	case "show":
		err = runShow(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func defaultDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".husonym", "ee-signing")
	}
	return "."
}

func addCommonFlags(fs *flag.FlagSet) (keyPath, registryPath *string) {
	dir := defaultDir()
	keyPath = fs.String("key", filepath.Join(dir, "husonym_ee_ca.key"),
		"PEM-encoded Ed25519 signing key (kept outside the repository)")
	registryPath = fs.String("registry", filepath.Join(dir, "registry.json"),
		"registry of issued licenses")
	return keyPath, registryPath
}

type intFlag struct{ v *int }

func (f *intFlag) String() string { return "" }
func (f *intFlag) Set(s string) error {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return fmt.Errorf("not a number: %q", s)
	}
	f.v = &n
	return nil
}

func runIssue(args []string) error {
	fs := flag.NewFlagSet("issue", flag.ExitOnError)
	keyPath, registryPath := addCommonFlags(fs)
	to := fs.String("to", "", "customer name (required)")
	customerId := fs.String("customer-id", "", "stable customer identifier (required)")
	days := fs.Int("days", 365, "days until expiry")
	id := fs.String("id", "", "license id (defaults to a random one)")
	note := fs.String("note", "", "free-form note: contract reference, ticket, …")
	connTypes := fs.String("connection-types", "",
		"comma-separated allowlist of connection types (empty means all)")
	dryRun := fs.Bool("dry-run", false, "validate and print without touching the registry")

	grace := &intFlag{}
	fs.Var(grace, "grace-days", fmt.Sprintf("grace days after expiry (default %d)", license.DefaultGraceDays))
	maxJobs := &intFlag{}
	fs.Var(maxJobs, "max-jobs", "maximum jobs (omit for unlimited)")
	maxConns := &intFlag{}
	fs.Var(maxConns, "max-connections", "maximum connections (omit for unlimited)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	keyBytes, source, err := loadSigningKey(*keyPath)
	if err != nil {
		return err
	}
	priv, err := license.ParsePrivateKey(keyBytes)
	if err != nil {
		return fmt.Errorf("signing key from %s: %w", source, err)
	}

	// Catch the failure that would otherwise only surface at the customer's site: a
	// license minted with a key this build does not verify against.
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return fmt.Errorf("signing key does not expose an ed25519 public key")
	}
	embedded, err := license.EmbeddedPublicKey()
	if err != nil {
		return fmt.Errorf("unable to read the embedded public key: %w", err)
	}
	if !embedded.Equal(pub) {
		return fmt.Errorf(
			"signing key does not match the key embedded in this build\n"+
				"  signing key : %s\n"+
				"  embedded    : %s\n"+
				"licenses minted with it would be rejected by the product",
			license.PublicKeyFingerprint(pub), license.PublicKeyFingerprint(embedded))
	}

	var limits *license.Limits
	types := splitList(*connTypes)
	if maxJobs.v != nil || maxConns.v != nil || len(types) > 0 {
		limits = &license.Limits{
			MaxJobs:                maxJobs.v,
			MaxConnections:         maxConns.v,
			AllowedConnectionTypes: types,
		}
	}

	issued, err := license.Issue(license.IssueRequest{
		Id:         *id,
		IssuedTo:   *to,
		CustomerId: *customerId,
		ExpiresAt:  time.Now().UTC().Add(time.Duration(*days) * 24 * time.Hour),
		GraceDays:  grace.v,
		Limits:     limits,
	}, priv)
	if err != nil {
		return err
	}

	if !*dryRun {
		reg, err := license.LoadRegistry(*registryPath)
		if err != nil {
			return err
		}
		if err := reg.Add(license.RegistryEntry{
			Id:             issued.Id,
			IssuedTo:       issued.IssuedTo,
			CustomerId:     issued.CustomerId,
			IssuedAt:       issued.IssuedAt,
			ExpiresAt:      issued.ExpiresAt,
			GraceDays:      issued.GraceDays,
			Limits:         issued.Limits,
			Encoded:        issued.Encoded,
			KeyFingerprint: license.PublicKeyFingerprint(pub),
			Note:           *note,
		}); err != nil {
			return err
		}
		if err := reg.Save(*registryPath); err != nil {
			return err
		}
	}

	fmt.Printf("issued %s to %s (customer %s)\n", issued.Id, issued.IssuedTo, issued.CustomerId)
	fmt.Printf("  expires    %s\n", issued.ExpiresAt.Format(time.RFC3339))
	fmt.Printf("  grace      %s\n", graceLabel(issued.GraceDays))
	fmt.Printf("  limits     %s\n", limitsLabel(issued.Limits))
	if *dryRun {
		fmt.Printf("  registry   not written (--dry-run)\n")
	} else {
		fmt.Printf("  registry   %s\n", *registryPath)
	}
	fmt.Printf("\nEE_LICENSE=%s\n", issued.Encoded)
	return nil
}

func runList(args []string, expiringOnly bool) error {
	name := "list"
	if expiringOnly {
		name = "expiring"
	}
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	_, registryPath := addCommonFlags(fs)
	within := fs.Int("within", 45, "days ahead to consider (expiring only)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	reg, err := license.LoadRegistry(*registryPath)
	if err != nil {
		return err
	}

	entries := reg.Entries
	if expiringOnly {
		entries = reg.ExpiringWithin(time.Duration(*within) * 24 * time.Hour)
	}
	if len(entries) == 0 {
		if expiringOnly {
			fmt.Printf("nothing expiring within %d days\n", *within)
		} else {
			fmt.Printf("no licenses recorded in %s\n", *registryPath)
		}
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STATE\tID\tCUSTOMER\tISSUED TO\tEXPIRES\tIN\tLIMITS")
	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			e.State(), e.Id, e.CustomerId, truncate(e.IssuedTo, 24),
			e.ExpiresAt.Format("2006-01-02"), humanDays(e.ExpiresAt), limitsLabel(e.Limits))
	}
	if err := w.Flush(); err != nil {
		return err
	}

	if !expiringOnly {
		if frozen := reg.Frozen(); len(frozen) > 0 {
			fmt.Printf("\n%d frozen (past grace)\n", len(frozen))
		}
	}
	return nil
}

func runShow(args []string) error {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	_, registryPath := addCommonFlags(fs)
	asJSON := fs.Bool("json", false, "print the raw registry entry")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: show <license-id>")
	}

	reg, err := license.LoadRegistry(*registryPath)
	if err != nil {
		return err
	}
	entry, ok := reg.Find(fs.Arg(0))
	if !ok {
		return fmt.Errorf("no license %q in %s", fs.Arg(0), *registryPath)
	}

	if *asJSON {
		out, err := json.MarshalIndent(entry, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}

	fmt.Printf("%s — %s (customer %s)\n", entry.Id, entry.IssuedTo, entry.CustomerId)
	fmt.Printf("  state      %s\n", entry.State())
	fmt.Printf("  issued     %s\n", entry.IssuedAt.Format(time.RFC3339))
	fmt.Printf("  expires    %s (%s)\n", entry.ExpiresAt.Format(time.RFC3339), humanDays(entry.ExpiresAt))
	fmt.Printf("  grace      %s\n", graceLabel(entry.GraceDays))
	fmt.Printf("  limits     %s\n", limitsLabel(entry.Limits))
	fmt.Printf("  signed by  %s\n", entry.KeyFingerprint)
	if entry.Note != "" {
		fmt.Printf("  note       %s\n", entry.Note)
	}
	fmt.Printf("\nEE_LICENSE=%s\n", entry.Encoded)
	return nil
}

func runVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: verify <EE_LICENSE value>")
	}
	// Verifies through exactly the path the product uses at startup.
	ee, err := license.NewFromValue(fs.Arg(0))
	if err != nil {
		return fmt.Errorf("license does not verify against this build: %w", err)
	}
	fmt.Printf("verifies against this build\n")
	fmt.Printf("  state    %s\n", ee.State())
	fmt.Printf("  expires  %s (%s)\n", ee.ExpiresAt().Format(time.RFC3339), humanDays(ee.ExpiresAt()))
	fmt.Printf("  grace to %s\n", ee.GracePeriodEndsAt().Format(time.RFC3339))
	fmt.Printf("  usable   %t\n", ee.IsValid())
	fmt.Printf("  limits   %s\n", limitsLabel(ee.Limits()))
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func graceLabel(days *int) string {
	if days == nil {
		return fmt.Sprintf("%d days (default)", license.DefaultGraceDays)
	}
	if *days == 0 {
		return "none"
	}
	return fmt.Sprintf("%d days", *days)
}

func limitsLabel(l *license.Limits) string {
	if l == nil {
		return "unlimited"
	}
	var parts []string
	if l.MaxJobs != nil {
		parts = append(parts, fmt.Sprintf("jobs=%d", *l.MaxJobs))
	}
	if l.MaxConnections != nil {
		parts = append(parts, fmt.Sprintf("connections=%d", *l.MaxConnections))
	}
	if len(l.AllowedConnectionTypes) > 0 {
		parts = append(parts, "types="+strings.Join(l.AllowedConnectionTypes, "|"))
	}
	if len(parts) == 0 {
		return "unlimited"
	}
	return strings.Join(parts, " ")
}

func humanDays(t time.Time) string {
	d := time.Until(t)
	days := int(d.Hours() / 24)
	switch {
	case days > 1:
		return fmt.Sprintf("in %d days", days)
	case days == 1 || (days == 0 && d > 0):
		return "within a day"
	case days == 0:
		return "today"
	case days == -1:
		return "1 day ago"
	default:
		return fmt.Sprintf("%d days ago", -days)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

const signingKeyEnv = "HUSONYM_EE_SIGNING_KEY"

// loadSigningKey prefers the environment over the filesystem, so the key can be injected
// by a secret manager and never written to disk:
//
//	infisical run -- go run ./internal/ee/license/cmd/husonym-license issue …
//
// Accepts the PEM directly or base64 of it. Both because a multi-line value survives some
// secret managers and shells intact and not others, and a key that fails to load at the
// moment you need to issue a licence is a bad time to discover which kind you have.
//
// Returns the source alongside the bytes purely so errors can say where the key came from.
func loadSigningKey(keyPath string) (key []byte, source string, err error) {
	if raw := strings.TrimSpace(os.Getenv(signingKeyEnv)); raw != "" {
		if strings.HasPrefix(raw, "-----BEGIN") {
			return []byte(raw), "$" + signingKeyEnv, nil
		}
		decoded, derr := base64.StdEncoding.DecodeString(raw)
		if derr != nil {
			return nil, "", fmt.Errorf(
				"$%s is set but is neither PEM nor base64-encoded PEM", signingKeyEnv)
		}
		return decoded, "$" + signingKeyEnv + " (base64)", nil
	}

	bytes, rerr := os.ReadFile(keyPath)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return nil, "", fmt.Errorf(
				"no signing key: %s is unset and %s does not exist\n"+
					"  supply one with --key, or inject it as $%s (PEM or base64)",
				signingKeyEnv, keyPath, signingKeyEnv)
		}
		return nil, "", fmt.Errorf("unable to read signing key at %s: %w", keyPath, rerr)
	}
	return bytes, keyPath, nil
}
