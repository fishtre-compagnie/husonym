package v1alpha1_accounthookservice

import (
	"context"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/internal/hooks"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
)

// Service is the account hook service of the contract: the procedures of the hooks, which
// are the hook logic's own, and those of the Slack kind. The Slack kind of account hook is
// retired: its procedures stay in the contract and answer as not implemented.
type Service struct {
	*hooks.AccountService
}

var _ mgmtv1alpha1connect.AccountHookServiceHandler = (*Service)(nil)

func New(accountHooks *hooks.AccountService) *Service {
	return &Service{AccountService: accountHooks}
}

func (s *Service) GetSlackConnectionUrl(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetSlackConnectionUrlRequest],
) (*connect.Response[mgmtv1alpha1.GetSlackConnectionUrlResponse], error) {
	return nil, husonymerrors.NewNotImplementedProcedure(
		mgmtv1alpha1connect.AccountHookServiceGetSlackConnectionUrlProcedure,
	)
}

func (s *Service) HandleSlackOAuthCallback(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.HandleSlackOAuthCallbackRequest],
) (*connect.Response[mgmtv1alpha1.HandleSlackOAuthCallbackResponse], error) {
	return nil, husonymerrors.NewNotImplementedProcedure(
		mgmtv1alpha1connect.AccountHookServiceHandleSlackOAuthCallbackProcedure,
	)
}

func (s *Service) TestSlackConnection(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.TestSlackConnectionRequest],
) (*connect.Response[mgmtv1alpha1.TestSlackConnectionResponse], error) {
	return nil, husonymerrors.NewNotImplementedProcedure(
		mgmtv1alpha1connect.AccountHookServiceTestSlackConnectionProcedure,
	)
}

func (s *Service) SendSlackMessage(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.SendSlackMessageRequest],
) (*connect.Response[mgmtv1alpha1.SendSlackMessageResponse], error) {
	return nil, husonymerrors.NewNotImplementedProcedure(
		mgmtv1alpha1connect.AccountHookServiceSendSlackMessageProcedure,
	)
}
