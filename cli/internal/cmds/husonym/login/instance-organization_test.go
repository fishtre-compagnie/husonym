package login_cmd

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

// systemOfTest answers the system information the test tells it to.
type systemOfTest struct {
	organizationId *string
	err            error
}

func (s systemOfTest) GetSystemInformation(
	context.Context,
	*connect.Request[mgmtv1alpha1.GetSystemInformationRequest],
) (*connect.Response[mgmtv1alpha1.GetSystemInformationResponse], error) {
	if s.err != nil {
		return nil, s.err
	}
	return connect.NewResponse(&mgmtv1alpha1.GetSystemInformationResponse{
		InstanceOrganizationAccountId: s.organizationId,
	}), nil
}

func Test_instanceOrganizationId(t *testing.T) {
	ctx := context.Background()
	organizationId := "o"

	t.Run("the organization the instance retains", func(t *testing.T) {
		var logged bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logged, nil))

		require.Equal(t, "o", instanceOrganizationId(ctx, systemOfTest{organizationId: &organizationId}, logger))
		require.Empty(t, instanceOrganizationId(ctx, systemOfTest{}, logger))
		require.Empty(t, logged.String())
	})

	// The login is done by then: not knowing the organization is told, and does not undo it.
	// The account is then picked as on an instance that retains none.
	t.Run("an instance that does not answer is a warning, and no organization", func(t *testing.T) {
		var logged bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logged, nil))
		personal := &mgmtv1alpha1.UserAccount{Id: "p", Name: "personal"}
		team := &mgmtv1alpha1.UserAccount{Id: "o", Name: "organization"}

		id := instanceOrganizationId(ctx, systemOfTest{err: errors.New("the instance is unreachable")}, logger)

		require.Empty(t, id)
		require.Contains(t, logged.String(), "level=WARN")
		require.Contains(t, logged.String(), "the instance is unreachable")
		require.Same(t, personal, defaultAccount([]*mgmtv1alpha1.UserAccount{team, personal}, id))
		require.Same(t, team, defaultAccount([]*mgmtv1alpha1.UserAccount{team}, id))
	})
}
