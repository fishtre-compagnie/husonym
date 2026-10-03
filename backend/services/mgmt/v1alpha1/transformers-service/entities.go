package v1alpha1_transformersservice

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	logger_interceptor "github.com/fishtre-compagnie/husonym/backend/internal/connect/interceptors/logger"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
)

// fallbackEntityLanguage is the language of a deployment that configures none.
const fallbackEntityLanguage = "en"

func (s *Service) GetTransformPiiEntities(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetTransformPiiEntitiesRequest],
) (*connect.Response[mgmtv1alpha1.GetTransformPiiEntitiesResponse], error) {
	if !s.cfg.IsPresidioEnabled {
		return nil, husonymerrors.NewNotImplemented(
			fmt.Sprintf(
				"%s is not implemented",
				strings.TrimPrefix(
					mgmtv1alpha1connect.TransformersServiceGetTransformPiiEntitiesProcedure,
					"/",
				),
			),
		)
	}
	if s.entityclient == nil {
		return nil, husonymerrors.NewInternalError("entity service is enabled but client was nil.")
	}
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	err = user.EnforceJob(
		ctx,
		userdata.NewWildcardDomainEntity(req.Msg.GetAccountId()),
		rbac.JobAction_View,
	)
	if err != nil {
		return nil, err
	}

	entities, err := s.entityclient.SupportedEntities(ctx, s.entityLanguage())
	if err != nil {
		answer := husonymerrors.FromPresidio(
			ctx,
			fmt.Errorf("unable to retrieve available entities: %w", err),
		)
		if husonymerrors.IsServiceFault(answer) {
			// Why is logged, and not told: the error can quote where Presidio is reached.
			logger_interceptor.GetLoggerFromContextOrDefault(ctx).
				Error("unable to retrieve available entities", "error", err)
		}
		return nil, answer
	}

	return connect.NewResponse(&mgmtv1alpha1.GetTransformPiiEntitiesResponse{
		Entities: entities,
	}), nil
}

// entityLanguage is the language the entities are listed for: the one a transformer that sets
// none analyzes in.
func (s *Service) entityLanguage() string {
	if s.cfg.PresidioDefaultLanguage != nil && *s.cfg.PresidioDefaultLanguage != "" {
		return *s.cfg.PresidioDefaultLanguage
	}
	return fallbackEntityLanguage
}
