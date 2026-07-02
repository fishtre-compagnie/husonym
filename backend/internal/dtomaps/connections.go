package dtomaps

import (
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ToConnectionDto(
	input *db_queries.HusonymApiConnection,
	canViewSensitive bool,
) (*mgmtv1alpha1.Connection, error) {
	ccDto, err := input.ConnectionConfig.ToDto(canViewSensitive)
	if err != nil {
		return nil, err
	}
	return &mgmtv1alpha1.Connection{
		Id:               husonymdb.UUIDString(input.ID),
		Name:             input.Name,
		ConnectionConfig: ccDto,
		CreatedAt:        timestamppb.New(input.CreatedAt.Time),
		UpdatedAt:        timestamppb.New(input.UpdatedAt.Time),
		CreatedByUserId:  husonymdb.UUIDString(input.CreatedByID),
		UpdatedByUserId:  husonymdb.UUIDString(input.UpdatedByID),
		AccountId:        husonymdb.UUIDString(input.AccountID),
	}, nil
}
