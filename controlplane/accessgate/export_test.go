package accessgate

// NewRemote lets the tests point the gate at a local key endpoint, with its client and a short
// interval of background refresh. It takes the config as it is: New is what checks one.
var NewRemote = newRemote

// The limits, so that the tests can stand on either side of them.
const (
	ClockSkew      = clockSkew
	MinRefreshGap  = minRefreshGap
	MaxKeySetBytes = maxKeySetBytes
)

// RefreshStopped is closed when the background refresh of a gate made by NewRemote has ended.
func RefreshStopped(g *Gate) <-chan struct{} {
	return g.keys.(*remoteKeys).stopped
}

// Checked gives a config as New would use it, without fetching anything.
func Checked(c Config) (Config, error) { return c.checked() }
