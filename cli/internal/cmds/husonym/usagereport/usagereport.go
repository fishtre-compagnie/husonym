package usagereport_cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/cli/internal/auth"
	cli_logger "github.com/fishtre-compagnie/husonym/cli/internal/logger"
	"github.com/spf13/cobra"
)

var monthPattern = regexp.MustCompile(`^\d{4}-(0[1-9]|1[012])$`)

// options holds the values of the flags of the command.
type options struct {
	from   string
	to     string
	output string
	force  bool
	print  bool
}

func NewCmd() *cobra.Command {
	var opts options
	cmd := &cobra.Command{
		Use:   "usage-report",
		Short: "Write the usage report of the instance for a period to a file",
		Long: `Write the usage report of the instance for a period of months to a file.

The file has two lines. The first is the report, exactly as the API returned it.
The second is a JSON object holding its seal and the fingerprint of the key that made it.
Use --print to read the report before the file is handed over.`,
		Example: "  husonym usage-report --from 2026-01 --to 2026-12 --output report.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			apiKey, err := cmd.Flags().GetString("api-key")
			if err != nil {
				return err
			}
			accountId, err := cmd.Flags().GetString("account-id")
			if err != nil {
				return err
			}
			debugMode, err := cmd.Flags().GetBool("debug")
			if err != nil {
				return err
			}
			// Nothing is asked of the API before the flags are known to be sound.
			if err := opts.validate(); err != nil {
				return err
			}
			cmd.SilenceUsage = true

			ctx := cmd.Context()
			logger := cli_logger.NewSLogger(cli_logger.GetCharmLevelOrDefault(debugMode))
			husonymurl := auth.GetHusonymUrl()
			httpclient, err := auth.GetHusonymHttpClient(ctx, logger, auth.WithApiKey(&apiKey))
			if err != nil {
				return err
			}
			userclient := mgmtv1alpha1connect.NewUserAccountServiceClient(httpclient, husonymurl)
			resolvedAccountId, err := auth.ResolveAccountIdFromFlag(
				ctx, userclient, &accountId, &apiKey, logger,
			)
			if err != nil {
				return err
			}
			usageclient := mgmtv1alpha1connect.NewUsageServiceClient(httpclient, husonymurl)
			return writeReport(
				ctx, usageclient, resolvedAccountId, opts, cmd.OutOrStdout(), cmd.ErrOrStderr(),
			)
		},
	}
	cmd.Flags().String("account-id", "", "Account to report on. Defaults to account id in cli context")
	cmd.Flags().StringVar(&opts.from, "from", "", "First month of the period, as YYYY-MM")
	cmd.Flags().StringVar(&opts.to, "to", "", "Last month of the period, as YYYY-MM")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "", "File to write the report to")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Overwrite the output file if it exists")
	cmd.Flags().BoolVar(&opts.print, "print", false, "Print the report, indented, to standard output")
	return cmd
}

func (o options) validate() error {
	if !monthPattern.MatchString(o.from) {
		return fmt.Errorf("--from is required and must be a month as YYYY-MM, got %q", o.from)
	}
	if !monthPattern.MatchString(o.to) {
		return fmt.Errorf("--to is required and must be a month as YYYY-MM, got %q", o.to)
	}
	// Both are zero-padded, so the text order is the calendar order.
	if o.from > o.to {
		return fmt.Errorf("--from (%s) must not be after --to (%s)", o.from, o.to)
	}
	if o.output == "" && !o.print {
		return errors.New("at least one of --output and --print is required")
	}
	return nil
}

// seal is the second line of the file.
type seal struct {
	Seal           string `json:"seal"`
	KeyFingerprint string `json:"key_fingerprint"`
}

// writeReport asks for the report of the period, then writes the file and/or prints it.
func writeReport(
	ctx context.Context,
	client mgmtv1alpha1connect.UsageServiceClient,
	accountId string,
	opts options,
	stdout, stderr io.Writer,
) error {
	res, err := client.GetUsagePeriodReport(
		ctx,
		connect.NewRequest(&mgmtv1alpha1.GetUsagePeriodReportRequest{
			AccountId: accountId,
			FromMonth: opts.from,
			ToMonth:   opts.to,
		}),
	)
	if err != nil {
		return err
	}
	document := []byte(res.Msg.GetDocument())

	if opts.output != "" {
		content, err := fileContent(document, seal{
			Seal:           res.Msg.GetSeal(),
			KeyFingerprint: res.Msg.GetKeyFingerprint(),
		})
		if err != nil {
			return err
		}
		if err := createFile(opts.output, content, opts.force); err != nil {
			return err
		}
	}
	if opts.print {
		return printDocument(document, stdout, stderr)
	}
	return nil
}

// fileContent is the document as received, a newline, the seal object and a newline.
//
// It assumes the document is a single line: the API's compact JSON holds no raw newline
// (a newline inside a string is escaped). The reader of the file relies on that: the
// first line is the document byte for byte, which is what the seal on the second line covers.
func fileContent(document []byte, s seal) ([]byte, error) {
	var buf bytes.Buffer
	buf.Write(document)
	buf.WriteByte('\n')
	enc := json.NewEncoder(&buf) // appends the final newline
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, fmt.Errorf("unable to encode the seal: %w", err)
	}
	return buf.Bytes(), nil
}

// createFile puts content under path with mode 0600, whole or not at all: it is written to a
// temporary file next to the target, then put in place in one step, so a failed write never
// leaves a partial file under the target name, nor truncates a file that --force replaces.
// Without force it never replaces a file, even if one appears meanwhile.
func createFile(path string, content []byte, force bool) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".usage-report-*") // created 0600
	if err != nil {
		return fmt.Errorf("unable to create %s: %w", path, err)
	}
	defer func() {
		// Gone already once renamed; this only removes what a failure left behind.
		_ = os.Remove(tmp.Name())
	}()
	if err := writeAndClose(tmp, content); err != nil {
		return fmt.Errorf("unable to write %s: %w", path, err)
	}

	if force {
		if err := os.Rename(tmp.Name(), path); err != nil {
			return fmt.Errorf("unable to write %s: %w", path, err)
		}
		return nil
	}
	// Link fails if the target exists, which a rename would not.
	err = os.Link(tmp.Name(), path)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, os.ErrExist):
		return existsError(path)
	}
	// Hard links are not available here: create the target exclusively instead.
	return createExclusive(path, content)
}

func existsError(path string) error {
	return fmt.Errorf("%s already exists, use --force to overwrite it", path)
}

func writeAndClose(f *os.File, content []byte) error {
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// createExclusive is the fallback of createFile without hard links; it removes the target
// if the write fails.
func createExclusive(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return existsError(path)
		}
		return fmt.Errorf("unable to create %s: %w", path, err)
	}
	if err := writeAndClose(f, content); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("unable to write %s: %w", path, err)
	}
	return nil
}

// printDocument writes an indented copy of the document, which differs from it by whitespace only.
func printDocument(document []byte, stdout, stderr io.Writer) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, document, "", "  "); err != nil {
		fmt.Fprintln(stderr, "the report is not valid JSON, printing it as it is")
		buf.Reset()
		buf.Write(document)
	}
	buf.WriteByte('\n')
	_, err := stdout.Write(buf.Bytes())
	return err
}
