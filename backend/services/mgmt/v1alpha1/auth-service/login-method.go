package v1alpha1_authservice

import (
	"context"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/oidcprobe"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

// GetAccountLoginMethod tells an unauthenticated caller which provider an account signs in
// with, so that a sign-in can start against the right one.
//
// This is the question nothing else can answer: an OIDC flow begins with the client id of
// the right connector, so the tenant has to be known before anybody has proved anything.
// The account is designated by the link that was followed, which is the deterministic
// answer and the one that asks nobody to prove ownership of a domain.
//
// What it reveals, deliberately: which accounts have declared a provider. That is
// impersonal, and an attacker aiming at an account already knows the account. What it must
// never reveal is whether a person has an account -- so nothing about a person enters
// here, and an account that is unknown answers exactly like one that has declared nothing.
func (s *Service) GetAccountLoginMethod(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetAccountLoginMethodRequest],
) (*connect.Response[mgmtv1alpha1.GetAccountLoginMethodResponse], error) {
	// The deployment's own provider, which is what an account with no setting uses. It is
	// also the answer to a slug nobody recognizes: the two are indistinguishable from
	// outside, which is the property this endpoint is designed around.
	deploymentProvider := &mgmtv1alpha1.GetAccountLoginMethodResponse{}

	if s.db == nil {
		return connect.NewResponse(deploymentProvider), nil
	}

	method, err := s.db.Q.GetAccountLoginMethodBySlug(ctx, s.db.Db, req.Msg.GetAccountSlug())
	if err != nil && !husonymdb.IsNoRows(err) {
		return nil, err
	} else if err != nil && husonymdb.IsNoRows(err) {
		return connect.NewResponse(deploymentProvider), nil
	}

	// A setting missing either half cannot start a flow. Answering with half of one would
	// send the browser somewhere that refuses it, with no way to tell why.
	if method.Issuer == "" || method.ClientID == "" {
		return connect.NewResponse(deploymentProvider), nil
	}

	return connect.NewResponse(&mgmtv1alpha1.GetAccountLoginMethodResponse{
		Issuer:   method.Issuer,
		ClientId: method.ClientID,
	}), nil
}

// accountAuthorizationEndpoint returns the client and the authorization endpoint of the
// provider an account declared, or empty strings when it declared none.
//
// The endpoint is discovered rather than stored: an account names an issuer, and the
// standard says where the rest lives. Storing endpoints would be storing a copy of
// something the provider is entitled to change.
func (s *Service) accountAuthorizationEndpoint(
	ctx context.Context,
	accountSlug string,
) (clientId, authorizeUrl string, err error) {
	if s.db == nil {
		return "", "", nil
	}
	method, err := s.db.Q.GetAccountLoginMethodBySlug(ctx, s.db.Db, accountSlug)
	if err != nil && !husonymdb.IsNoRows(err) {
		return "", "", err
	} else if err != nil && husonymdb.IsNoRows(err) {
		return "", "", nil
	}
	if method.Issuer == "" || method.ClientID == "" {
		return "", "", nil
	}

	endpoint, err := oidcprobe.New().AuthorizationEndpoint(ctx, method.Issuer)
	if err != nil {
		return "", "", husonymerrors.NewBadRequest(
			"the identity provider of this account did not answer its discovery document: " + err.Error(),
		)
	}
	return method.ClientID, endpoint, nil
}
