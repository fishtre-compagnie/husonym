// Package licenseloader gives the worker's license provider the key the API holds.
package licenseloader

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	"github.com/fishtre-compagnie/husonym/internal/license"
)

// FromAPI asks the API for the signed key in force. The worker verifies it itself: the API
// only hands the value over. An instance that holds no key answers an empty value, which
// is not a failure.
func FromAPI(client mgmtv1alpha1connect.UserAccountServiceClient) license.Loader {
	return func(ctx context.Context) (string, error) {
		resp, err := client.GetSystemLicenseKey(ctx, connect.NewRequest(&mgmtv1alpha1.GetSystemLicenseKeyRequest{}))
		if err != nil {
			return "", fmt.Errorf("unable to get the license key from the API: %w", err)
		}
		return resp.Msg.GetKey(), nil
	}
}
