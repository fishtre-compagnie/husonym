package accessgate

// NewRemote lets the tests point the gate at a local key endpoint, with its client and a short
// interval of background refresh.
var NewRemote = newRemote

// ClockSkew and MinRefreshGap let the tests stand on either side of the limits.
const (
	ClockSkew     = clockSkew
	MinRefreshGap = minRefreshGap
)
