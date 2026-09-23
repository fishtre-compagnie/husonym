package auth0

import (
	"context"

	"github.com/auth0/go-auth0/v3/management"
	"github.com/auth0/go-auth0/v3/management/client"
	"github.com/auth0/go-auth0/v3/management/option"
	"github.com/fishtre-compagnie/husonym/internal/authmgmt"
)

var _ authmgmt.Interface = &Auth0MgmtClient{} // ensures it always conforms to the interface

type Auth0MgmtClient struct {
	client *client.Management
}

func New(domain, clientId, clientSecret string) (*Auth0MgmtClient, error) {
	mgmt, err := client.New(
		domain,
		option.WithClientCredentials(context.Background(), clientId, clientSecret),
	)
	if err != nil {
		return nil, err
	}
	return &Auth0MgmtClient{
		client: mgmt,
	}, nil
}

func (c *Auth0MgmtClient) GetUserBySub(ctx context.Context, id string) (*authmgmt.User, error) {
	user, err := c.client.Users.Get(ctx, id, &management.GetUserRequestParameters{})
	if err != nil {
		return nil, err
	}
	return &authmgmt.User{
		Name:          user.GetName(),
		Email:         user.GetEmail(),
		EmailVerified: user.GetEmailVerified(),
		Picture:       user.GetPicture(),
	}, nil
}
