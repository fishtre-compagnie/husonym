package usagestore

import (
	"context"
	"fmt"
	"time"

	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
)

// UserSeen notes that a user was seen on the UTC day of the given time. A day older than the one
// already noted changes nothing.
func (s *Store) UserSeen(ctx context.Context, userId string, day time.Time) error {
	user, err := husonymdb.ToUuid(userId)
	if err != nil {
		return fmt.Errorf("user id: %w", err)
	}
	return s.db.Q.UpsertUserActivity(ctx, s.db.Db, db_queries.UpsertUserActivityParams{
		UserID:     user,
		LastSeenOn: utcDate(day),
	})
}

// UsersSeenSince counts the users last seen on the UTC day of the given time or after it.
func (s *Store) UsersSeenSince(ctx context.Context, since time.Time) (int64, error) {
	return s.db.Q.CountUsersSeenSince(ctx, s.db.Db, utcDate(since))
}
