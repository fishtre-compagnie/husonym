package transformer_executor

import (
	"context"
	"log/slog"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	ee_transformer_fns "github.com/fishtre-compagnie/husonym/internal/ee/transformers/functions"
	"github.com/fishtre-compagnie/husonym/worker/pkg/benthos/transformers"
)

type piiTextApi struct {
	execConfig         *transformPiiTextConfig
	husonymOperatorApi ee_transformer_fns.HusonymOperatorApi
	logger             *slog.Logger
}

func newFromExecConfig(
	execConfig *transformPiiTextConfig,
	husonymOperatorApi ee_transformer_fns.HusonymOperatorApi,
	logger *slog.Logger,
) transformers.TransformPiiTextApi {
	return &piiTextApi{
		execConfig:         execConfig,
		husonymOperatorApi: husonymOperatorApi,
		logger:             logger,
	}
}

func (p *piiTextApi) Transform(
	ctx context.Context,
	config *mgmtv1alpha1.TransformPiiText,
	value string,
) (string, error) {
	return ee_transformer_fns.TransformPiiText(
		ctx,
		p.execConfig.analyze,
		p.execConfig.anonymize,
		p.husonymOperatorApi,
		config,
		value,
		p.logger,
	)
}
