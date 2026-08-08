package license

import "time"

// LimitedLicense is implemented by licenses carrying usage caps.
//
// Deliberately a separate interface, discovered by type assertion, rather than a method
// added to EEInterface: six types implement that interface (including two test fakes and
// the cloud license) and none of them has anything to say about limits. Widening it would
// have forced a stub onto all of them.
type LimitedLicense interface {
	Limits() *Limits
}

// LimitsOf returns the caps a license carries, or nil when it carries none — which means
// uncapped, not zero. Callers must not treat nil as "everything forbidden".
func LimitsOf(l EEInterface) *Limits {
	limited, ok := l.(LimitedLicense)
	if !ok {
		return nil
	}
	return limited.Limits()
}

type CascadeLicense struct {
	isValid   bool
	expiresAt time.Time
	limits    *Limits
}

var (
	_ EEInterface    = (*CascadeLicense)(nil)
	_ LimitedLicense = (*CascadeLicense)(nil)
)

// Checks multiple licenses in input order to see if any are valid
func NewCascadeLicense(licenses ...EEInterface) *CascadeLicense {
	isValid := false
	expiresAt := time.Time{}
	var limits *Limits
	for _, l := range licenses {
		if l.IsValid() {
			isValid = true
			expiresAt = l.ExpiresAt()
			// Carry the caps of the license actually granting access, so a caller holding
			// the cascade can enforce them without knowing which entry won.
			limits = LimitsOf(l)
			break
		}
	}
	return &CascadeLicense{isValid: isValid, expiresAt: expiresAt, limits: limits}
}

func (c *CascadeLicense) Limits() *Limits {
	return c.limits
}

func (c *CascadeLicense) IsValid() bool {
	return c.isValid
}

func (c *CascadeLicense) ExpiresAt() time.Time {
	return c.expiresAt
}
