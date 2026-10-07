package telemetry

import (
	"strings"

	"github.com/fishtre-compagnie/husonym/internal/license"
)

// Mode is how the usage report of the instance is handled.
type Mode string

const (
	// ModeOnline prepares the report and sends it to the address it is sent to.
	ModeOnline Mode = "online"
	// ModeOfflineReport prepares the report and keeps it to be handed over by hand.
	ModeOfflineReport Mode = "offline_report"
	// ModeNone neither prepares nor sends anything.
	ModeNone Mode = "none"
)

// EffectiveMode gives the mode in force and whether the setting lowers it below what the key
// provides. The setting is the raw value of HUSONYM_TELEMETRY: "offline" and "off" lower the
// mode, any other value (empty included) counts as not set. The setting never raises it.
func EffectiveMode(key license.TelemetryMode, setting string) (mode Mode, belowLicense bool) {
	var provided Mode
	switch key {
	case license.TelemetryNone:
		return ModeNone, false
	case license.TelemetryOfflineReport:
		provided = ModeOfflineReport
	default:
		provided = ModeOnline
	}
	switch strings.ToLower(strings.TrimSpace(setting)) {
	case "off":
		return ModeNone, true
	case "offline":
		return ModeOfflineReport, provided != ModeOfflineReport
	}
	return provided, false
}
