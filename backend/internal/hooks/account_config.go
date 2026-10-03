package hooks

import (
	"fmt"
	"net/url"
	"strings"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// accountKind is the kind of an account hook, as its configuration tells it.
type accountKind int

const (
	// kindNone is a configuration that holds no kind this version knows.
	kindNone accountKind = iota
	kindWebhook
	// kindSlack is retired: a hook of that kind is still listed, turned off and deleted,
	// and is neither created nor turned on.
	kindSlack
)

func kindOf(config *mgmtv1alpha1.AccountHookConfig) accountKind {
	switch config.GetConfig().(type) {
	case *mgmtv1alpha1.AccountHookConfig_Webhook:
		return kindWebhook
	case *mgmtv1alpha1.AccountHookConfig_Slack:
		return kindSlack
	default:
		return kindNone
	}
}

func slackRetired() error {
	return husonymerrors.NewBadRequest("Slack account hooks are no longer supported: use a webhook instead")
}

// refuseRetired refuses a request that carries a configuration of the retired kind.
func refuseRetired(config *mgmtv1alpha1.AccountHookConfig) func() error {
	return func() error {
		if kindOf(config) == kindSlack {
			return slackRetired()
		}
		return nil
	}
}

// refuseToArm refuses to turn on a hook the worker would not run: one of the retired kind,
// or one whose configuration holds no kind this version knows.
func refuseToArm(row *db_queries.HusonymApiAccountHook) func() error {
	return func() error {
		config, err := decodeAccountConfig(row)
		if err != nil {
			return err
		}
		switch kindOf(config) {
		case kindWebhook:
			return nil
		case kindSlack:
			return slackRetired()
		default:
			return husonymerrors.NewBadRequest("account hook has no supported configuration: update it with a webhook")
		}
	}
}

// storedAccountHook is what an account hook holds, checked and in the form the database
// stores.
type storedAccountHook struct {
	config []byte
	events []int32
}

// checkAccountHook checks what a request gives an account hook: a webhook, called over http
// or https at a host, signed with a secret that is not the mask, on events that exist.
func checkAccountHook(
	config *mgmtv1alpha1.AccountHookConfig,
	events []mgmtv1alpha1.AccountHookEvent,
) (*storedAccountHook, error) {
	webhook := config.GetWebhook()
	if webhook == nil {
		return nil, husonymerrors.NewBadRequest("account hook config is required: a webhook")
	}
	numbers := make([]int32, 0, len(events))
	for _, event := range events {
		if _, ok := mgmtv1alpha1.AccountHookEvent_name[int32(event)]; !ok {
			return nil, husonymerrors.NewBadRequest(fmt.Sprintf("invalid event: %d", event))
		}
		numbers = append(numbers, int32(event))
	}
	if !isHTTPAddress(webhook.GetUrl()) {
		return nil, husonymerrors.NewBadRequest("webhook url must be an http or https address")
	}
	if err := refuseMaskedSecret(webhook); err != nil {
		return nil, err
	}
	encoded, err := protojson.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize the account hook config: %w", err)
	}
	return &storedAccountHook{config: encoded, events: numbers}, nil
}

// isHTTPAddress says whether a URL is one the worker can call: http or https, at a host.
func isHTTPAddress(raw string) bool {
	address, err := url.Parse(raw)
	if err != nil || address.Hostname() == "" {
		return false
	}
	scheme := strings.ToLower(address.Scheme)
	return scheme == "http" || scheme == "https"
}

// decodeAccountConfig reads a configuration the way the database holds it.
func decodeAccountConfig(row *db_queries.HusonymApiAccountHook) (*mgmtv1alpha1.AccountHookConfig, error) {
	config := &mgmtv1alpha1.AccountHookConfig{}
	if err := stored.Unmarshal(row.Config, config); err != nil {
		return nil, fmt.Errorf("unable to read the config of account hook %s: %w", husonymdb.UUIDString(row.ID), err)
	}
	return config, nil
}

// toAccountHook gives a hook as a caller reads it: with its secret, or with the mask in its
// place.
func toAccountHook(row *db_queries.HusonymApiAccountHook, readsSecret bool) (*mgmtv1alpha1.AccountHook, error) {
	config, err := decodeAccountConfig(row)
	if err != nil {
		return nil, err
	}
	if !readsSecret {
		maskSecret(config)
	}
	events := make([]mgmtv1alpha1.AccountHookEvent, 0, len(row.Events))
	for _, event := range row.Events {
		if _, ok := mgmtv1alpha1.AccountHookEvent_name[event]; ok {
			events = append(events, mgmtv1alpha1.AccountHookEvent(event))
		}
	}
	return &mgmtv1alpha1.AccountHook{
		Id:              husonymdb.UUIDString(row.ID),
		Name:            row.Name,
		Description:     row.Description,
		AccountId:       husonymdb.UUIDString(row.AccountID),
		Events:          events,
		Config:          config,
		CreatedByUserId: husonymdb.UUIDString(row.CreatedByUserID),
		CreatedAt:       timestamppb.New(row.CreatedAt.Time),
		UpdatedByUserId: husonymdb.UUIDString(row.UpdatedByUserID),
		UpdatedAt:       timestamppb.New(row.UpdatedAt.Time),
		Enabled:         row.Enabled,
	}, nil
}

func toAccountHooks(rows []db_queries.HusonymApiAccountHook, readsSecret bool) ([]*mgmtv1alpha1.AccountHook, error) {
	hooks := make([]*mgmtv1alpha1.AccountHook, 0, len(rows))
	for i := range rows {
		hook, err := toAccountHook(&rows[i], readsSecret)
		if err != nil {
			return nil, err
		}
		hooks = append(hooks, hook)
	}
	return hooks, nil
}
