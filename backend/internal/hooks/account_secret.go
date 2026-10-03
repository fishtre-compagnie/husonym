package hooks

import (
	"context"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	pg_models "github.com/fishtre-compagnie/husonym/backend/sql/postgresql/models"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
)

// The secret of a webhook signs what the worker sends. It is read by the worker, which
// signs, and by whoever may edit the account: the one who set it and can replace it. Any
// other caller reads the mask in its place.

// readsSecret says whether an admitted caller reads the secrets of the hooks of the account.
func (a *admission) readsSecret(ctx context.Context) (bool, error) {
	if a.caller.IsWorkerApiKey() {
		return true, nil
	}
	return a.caller.Account(ctx, userdata.NewIdentifier(a.accountID), rbac.AccountAction_Edit)
}

// maskSecret puts the mask in the place of the secret of a webhook.
func maskSecret(config *mgmtv1alpha1.AccountHookConfig) {
	if webhook := config.GetWebhook(); webhook.GetSecret() != "" {
		webhook.Secret = pg_models.SensitiveValue
	}
}

// refuseMaskedSecret refuses a webhook whose secret is the mask: it was read masked and sent
// back. Stored, the mask would replace the secret; and the stored secret is not kept in its
// place, as a write says the whole of what a hook holds.
func refuseMaskedSecret(webhook *mgmtv1alpha1.AccountHookConfig_WebHook) error {
	if webhook.GetSecret() == pg_models.SensitiveValue {
		return husonymerrors.NewBadRequest("the secret of the webhook is masked: send the secret itself")
	}
	return nil
}
