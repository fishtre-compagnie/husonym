package v1alpha1_transformersservice

import (
	"context"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/transformers/catalog"
)

func (s *Service) GetSystemTransformers(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetSystemTransformersRequest],
) (*connect.Response[mgmtv1alpha1.GetSystemTransformersResponse], error) {
	systemTransformers := s.getSystemTransformers()
	return connect.NewResponse(&mgmtv1alpha1.GetSystemTransformersResponse{
		Transformers: systemTransformers,
	}), nil
}

func (s *Service) GetSystemTransformerBySource(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetSystemTransformerBySourceRequest],
) (*connect.Response[mgmtv1alpha1.GetSystemTransformerBySourceResponse], error) {
	transformerMap := s.getSystemTransformerSourceMap()

	transformer, ok := transformerMap[req.Msg.GetSource()]
	if !ok {
		return nil, husonymerrors.NewNotFound(
			"unable to find system transformer with provided source",
		)
	}
	return connect.NewResponse(&mgmtv1alpha1.GetSystemTransformerBySourceResponse{
		Transformer: transformer,
	}), nil
}

func (s *Service) getSystemTransformerSourceMap() map[mgmtv1alpha1.TransformerSource]*mgmtv1alpha1.SystemTransformer {
	return catalog.BySource(s.license.IsValid())
}

func (s *Service) getSystemTransformers() []*mgmtv1alpha1.SystemTransformer {
	return catalog.Transformers(s.license.IsValid())
}
