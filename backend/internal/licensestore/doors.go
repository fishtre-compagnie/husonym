package licensestore

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/viper"
)

// The environment no longer holds the key in force: EE_LICENSE and EE_LICENSE_FILE are doors
// through which a key is offered to the store, whose rule decides like for any other offer.

// OfferFromEnvironment offers the key of the file EE_LICENSE_FILE names and the key EE_LICENSE
// holds, both when both are set. Nothing here stops a start: what was done with each key is
// logged, and so is a file that cannot be read or a database that does not answer.
func OfferFromEnvironment(ctx context.Context, store *Store, logger *slog.Logger) {
	if path := viper.GetString("EE_LICENSE_FILE"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			logger.Error("the license file cannot be read", "path", path, "error", err)
		} else {
			_, _ = offer(ctx, store, string(raw), OriginFile, logger)
		}
	}
	if value := viper.GetString("EE_LICENSE"); value != "" {
		_, _ = offer(ctx, store, value, OriginEnvironment, logger)
	}
}

// WatchFile reads the license file again at the given interval and offers its content when it
// is not what was last offered, so that a key written to the file reaches the store without a
// restart. onAccepted is called once a key was accepted. It returns when ctx is done.
func WatchFile(
	ctx context.Context,
	store *Store,
	path string,
	every time.Duration,
	onAccepted func(),
	logger *slog.Logger,
) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	// offered is the content last offered; hasOffered tells it apart from an empty file.
	var offered string
	hasOffered := false
	// unreadable is the text of the last read failure logged, so that a file that stays
	// unreadable is logged once.
	unreadable := ""

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			if text := err.Error(); text != unreadable {
				unreadable = text
				logger.Error("the license file cannot be read", "path", path, "error", err)
			}
			continue
		}
		unreadable = ""

		content := string(raw)
		if hasOffered && content == offered {
			continue
		}
		result, err := offer(ctx, store, content, OriginFile, logger)
		if err != nil {
			// The database did not answer: nothing was decided, the content is offered again.
			continue
		}
		offered, hasOffered = content, true
		if result.Outcome == Accepted {
			onAccepted()
		}
	}
}

// offer puts a key forward and logs what the store decided. The key value is never logged.
func offer(
	ctx context.Context,
	store *Store,
	value string,
	origin Origin,
	logger *slog.Logger,
) (*Result, error) {
	logger = logger.With("origin", string(origin))
	result, err := store.Offer(ctx, value, origin, nil)
	if err != nil {
		logger.Error("the license key could not be offered to the database", "error", err)
		return nil, err
	}
	switch result.Outcome {
	case Accepted:
		logger.Info("the license key was accepted", "licenseId", result.Key.Id)
	case Unchanged:
		logger.Debug("the license key is the one already in force")
	case RefusedOlder:
		logger.Info("the license key is older than the one in force and is ignored", "reason", result.Reason)
	case RefusedInvalid:
		logger.Error("the license key is not valid", "reason", result.Reason)
	}
	return result, nil
}
