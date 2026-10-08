package cpstore

import "time"

// What the console and the gauges count as worth a look.
const (
	// SilentAfterDays: an instance is silent when its last report is of a day more than this
	// many days ago.
	SilentAfterDays = 3
	// RecentInstanceDays: an instance is seen lately when its last report is of a day no more
	// than this many days ago. One seen longer ago is no longer silent: it is gone.
	RecentInstanceDays = 30
	// ExpiringWithin: a license is expiring when it expires within this long.
	ExpiringWithin = 30 * 24 * time.Hour
	// OldPendingAfter: a pending report is old once received more than this long ago.
	OldPendingAfter = 24 * time.Hour
	// SealRejectionDays is how many days of refused seals a page lists, today included.
	SealRejectionDays = 7
	// InstanceReportsCap is how many reports of an instance are listed at most.
	InstanceReportsCap = 400
)
