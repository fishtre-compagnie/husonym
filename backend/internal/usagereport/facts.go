package usagereport

import (
	"log/slog"
	"runtime"
	"strings"

	"github.com/fishtre-compagnie/husonym/internal/telemetry"
	"github.com/spf13/viper"
)

const (
	// installKindVariable says how the instance is installed. The chart and the Compose file set
	// it; an instance started another way does not, and is of the kind other.
	installKindVariable = "HUSONYM_INSTALL_KIND"
	// diagnosticsVariable switches off the part of the report that describes the instance, when
	// it holds false. Any other value, or none, leaves it on.
	diagnosticsVariable = "HUSONYM_TELEMETRY_DIAGNOSTICS"
	// modeVariable lowers what the instance does with its usage report below what its license
	// provides: offline keeps the report for a report file, off keeps it. Any other value, or
	// none, leaves what the license provides.
	modeVariable = "HUSONYM_TELEMETRY"
	// reportURLVariable replaces the address the report is sent to, for a trial.
	reportURLVariable = "HUSONYM_TELEMETRY_URL"
)

// ModeSettingFromEnvironment gives the value of HUSONYM_TELEMETRY as it is written: what it
// means is told by telemetry.EffectiveMode, with what the license provides.
func ModeSettingFromEnvironment() string {
	return viper.GetString(modeVariable)
}

// ReportURLFromEnvironment gives the address the report is sent to: the one of
// HUSONYM_TELEMETRY_URL when it is one a report can be sent to, the default one otherwise. A
// value that is refused is logged, without the value: it may hold credentials.
func ReportURLFromEnvironment(logger *slog.Logger) string {
	address := strings.TrimSpace(viper.GetString(reportURLVariable))
	if address == "" {
		return DefaultReportURL
	}
	if _, err := checkReportURL(address); err != nil {
		logger.Warn(
			"the usage report is sent to its default address: the one given cannot be used",
			"variable", reportURLVariable, "error", err,
		)
		return DefaultReportURL
	}
	return address
}

// Facts is what the process knows of itself, read once at startup. Every text in it is already
// a member of its closed list of the telemetry package.
type Facts struct {
	Version, InstallKind, OS, Arch string
	AuthEnabled                    bool
	// AuthProvider is empty when authentication is off.
	AuthProvider string
	Presidio     bool
	RunLogs      string
	// Diagnostics is false when the operator switched the diagnostic off.
	Diagnostics bool
}

// FactsFromEnvironment gathers the facts from what the start of the API already resolved, which
// it takes as it is, and from the two variables nothing else reads.
//
// authProvider is the provider the deployment names, empty for the default one; runLogsType is
// where the logs of a run are read from, which counts only when they are enabled.
func FactsFromEnvironment(
	version string,
	authEnabled bool,
	authProvider string,
	presidio bool,
	runLogsEnabled bool,
	runLogsType string,
) Facts {
	facts := Facts{
		Version:     telemetry.HusonymVersion(version),
		InstallKind: telemetry.InstallKind(viper.GetString(installKindVariable)),
		OS:          telemetry.OperatingSystem(runtime.GOOS),
		Arch:        telemetry.Architecture(runtime.GOARCH),
		AuthEnabled: authEnabled,
		Presidio:    presidio,
		RunLogs:     telemetry.RunLogs(""),
		Diagnostics: !strings.EqualFold(strings.TrimSpace(viper.GetString(diagnosticsVariable)), "false"),
	}
	if authEnabled {
		// A deployment that names no provider runs on the default one.
		if strings.TrimSpace(authProvider) == "" {
			authProvider = "auth0"
		}
		facts.AuthProvider = telemetry.AuthProvider(authProvider)
	}
	if runLogsEnabled {
		facts.RunLogs = telemetry.RunLogs(runLogsType)
	}
	return facts
}
