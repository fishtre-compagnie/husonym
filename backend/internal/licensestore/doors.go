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

// offerTimeout bounds one offer made through a door, so that a database that accepts the
// connection and says nothing can neither hang a start nor keep a door busy.
const offerTimeout = 10 * time.Second

// OfferFromEnvironment offers the key of the file EE_LICENSE_FILE names and the key EE_LICENSE
// holds, both when both are set. Nothing here stops a start: what was done with each key is
// logged, and so is a file that cannot be read or a database that does not answer.
//
// It tells whether the store answered for the variable, which is also true when the variable
// is not set. The variable is read once, so a caller told false keeps offering it with
// OfferEnvironmentUntilAnswered. The file needs no such care: WatchFile offers it again.
func OfferFromEnvironment(ctx context.Context, store *Store, logger *slog.Logger) bool {
	if path := viper.GetString("EE_LICENSE_FILE"); path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			logger.Error("the license file cannot be read", "path", path, "error", err)
		} else {
			_, _ = offer(ctx, store, string(raw), OriginFile, logger)
		}
	}
	_, err := offerEnvironmentValue(ctx, store, logger)
	return err == nil
}

// offerEnvironmentValue offers the key EE_LICENSE holds. Without one there is nothing to
// offer, which is no error either.
func offerEnvironmentValue(ctx context.Context, store *Store, logger *slog.Logger) (*Result, error) {
	value := viper.GetString("EE_LICENSE")
	if value == "" {
		return nil, nil
	}
	return offer(ctx, store, value, OriginEnvironment, logger)
}

// OfferEnvironmentUntilAnswered offers the key EE_LICENSE holds at the given interval until
// the store has answered for it once, then returns: accepted, unchanged, older and invalid
// are all answers, a database that does not answer is not. onAccepted is called when the key
// was accepted. It also returns when ctx is done.
func OfferEnvironmentUntilAnswered(
	ctx context.Context,
	store *Store,
	every time.Duration,
	onAccepted func(),
	logger *slog.Logger,
) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		result, err := offerEnvironmentValue(ctx, store, logger)
		if err != nil {
			continue
		}
		if result != nil && result.Outcome == Accepted {
			onAccepted()
		}
		return
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
	ctx, cancel := context.WithTimeout(ctx, offerTimeout)
	defer cancel()
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
	case RefusedOtherCustomer:
		// Only a key received as a renewal is refused so, and none is offered through a door.
		logger.Error("the license key is not a renewal of the one in force", "reason", result.Reason)
	}
	return result, nil
}
