package authmgmt

import (
	"context"
	"errors"
)

// User is the display identity of a subject, as an identity provider presents it. These
// are the standard OIDC claims, so the same shape serves both sources: what the provider
// sends at sign-in, and what a provider's administration API answers later.
//
// It is display material. Nothing here decides anything: a user is identified by its
// provider subject, and EmailVerified is what an invite has to be judged on, never Email
// on its own.
type User struct {
	Name  string
	Email string
	// Whether the provider asserts it verified Email. False means no proof -- which is
	// also what an absent claim gives, and the two are deliberately indistinguishable.
	EmailVerified bool
	Picture       string
}

type Interface interface {
	GetUserBySub(ctx context.Context, sub string) (*User, error)
}

type UnimplementedClient struct{}

var _ Interface = &UnimplementedClient{}

func (c *UnimplementedClient) GetUserBySub(ctx context.Context, sub string) (*User, error) {
	return nil, errors.ErrUnsupported
}
