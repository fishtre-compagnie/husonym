package dtomaps

import (
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ToAccountApiKeyDto(
	input *db_queries.HusonymApiAccountApiKey,
	cleartextKeyValue *string,
) *mgmtv1alpha1.AccountApiKey {
	return &mgmtv1alpha1.AccountApiKey{
		Id:          husonymdb.UUIDString(input.ID),
		Name:        input.KeyName,
		AccountId:   husonymdb.UUIDString(input.AccountID),
		CreatedById: husonymdb.UUIDString(input.CreatedByID),
		CreatedAt:   timestamppb.New(input.CreatedAt.Time),
		UpdatedById: husonymdb.UUIDString(input.UpdatedByID),
		UpdatedAt:   timestamppb.New(input.UpdatedAt.Time),
		KeyValue:    cleartextKeyValue,
		UserId:      husonymdb.UUIDString(input.UserID),
		ExpiresAt:   timestamppb.New(input.ExpiresAt.Time),
	}
}
