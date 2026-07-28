package v1alpha1_metricsservice

import (
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

type Service struct {
	cfg *Config

	userdataclient   userdata.Interface
	jobservice       mgmtv1alpha1connect.JobServiceHandler
	prometheusclient promv1.API
}

type Config struct {
	IsAuthEnabled bool
}

func New(
	cfg *Config,
	userdataclient userdata.Interface,
	jobservice mgmtv1alpha1connect.JobServiceHandler,
	promclient promv1.API,
) *Service {
	return &Service{
		cfg:              cfg,
		userdataclient:   userdataclient,
		jobservice:       jobservice,
		prometheusclient: promclient,
	}
}
