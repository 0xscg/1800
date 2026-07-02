package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ Pool *pgxpool.Pool }

func New(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	return &Store{Pool: pool}, nil
}

// UpsertRawEvent stores/refreshes a provider payload. Idempotent by (provider, kind, external_id).
func (s *Store) UpsertRawEvent(ctx context.Context, provider, kind, externalID string, payload []byte) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO raw_events (provider, kind, external_id, payload)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (provider, kind, external_id)
		DO UPDATE SET payload = EXCLUDED.payload, received_at = now()`,
		provider, kind, externalID, payload)
	return err
}

// UpsertDailyMetric writes one normalized value. Idempotent by (day, metric, source).
func (s *Store) UpsertDailyMetric(ctx context.Context, day time.Time, metric, source string, value float64) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO daily_metrics (day, metric, source, value)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (day, metric, source) DO UPDATE SET value = EXCLUDED.value`,
		day, metric, source, value)
	return err
}
