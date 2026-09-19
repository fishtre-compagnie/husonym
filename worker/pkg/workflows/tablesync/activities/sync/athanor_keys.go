package sync_activity

import (
	"context"
	"fmt"

	"github.com/fishtre-compagnie/husonym/worker/pkg/athanor/runner"
	"github.com/redis/go-redis/v9"
)

// redisKeyStore keeps the transformed keys of a run in Redis hashes, one per referenced
// column, named like the ones Benthos uses: the clean-up activity of the workflow deletes
// them at the end of the run whichever engine wrote them.
type redisKeyStore struct {
	client redis.UniversalClient
}

// newRedisKeyStore returns nil without a Redis client: the runner then refuses the tables
// that need one, and only those.
func newRedisKeyStore(client redis.UniversalClient) runner.KeyStore {
	if client == nil {
		return nil
	}
	return &redisKeyStore{client: client}
}

func (s *redisKeyStore) Publish(ctx context.Context, store string, newBySource map[string][]byte) error {
	values := make([]any, 0, 2*len(newBySource))
	for source, encoded := range newBySource {
		values = append(values, source, encoded)
	}
	return s.client.HSet(ctx, store, values...).Err()
}

func (s *redisKeyStore) Lookup(ctx context.Context, store string, sources []string) ([][]byte, error) {
	found, err := s.client.HMGet(ctx, store, sources...).Result()
	if err != nil {
		return nil, err
	}
	values := make([][]byte, len(found))
	for i, value := range found {
		switch v := value.(type) {
		case nil:
		case string:
			values[i] = []byte(v)
		default:
			return nil, fmt.Errorf("unexpected redis value type %T", value)
		}
	}
	return values, nil
}
