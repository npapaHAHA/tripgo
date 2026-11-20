package trip

import (
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
	"github.com/npapaHAHA/tripgo/internal/infra/postgres/transaction"
)

const lastSeenExpression = "COALESCE(last_position_at, started_at)"

func (r *TripRepository) ListStale(ctx context.Context, staleAfter time.Duration, after *trip.StaleTrip, limit int) ([]trip.StaleTrip, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("list stale trips: limit must be positive")
	}
	queryBuilder := r.builder.Select("id", "driver_id", lastSeenExpression).
		From("trips").Where("status = 'active'").
		Where(sq.Expr(lastSeenExpression+" < CURRENT_TIMESTAMP - (? * INTERVAL '1 second')", staleAfter.Seconds())).
		OrderBy(lastSeenExpression+" ASC", "id ASC").Limit(uint64(limit))
	if after != nil {
		queryBuilder = queryBuilder.Where(sq.Expr("("+lastSeenExpression+", id) > (?, ?)", after.LastSeenAt, after.ID))
	}
	query, args, err := queryBuilder.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list stale trips query: %w", err)
	}
	rows, err := transaction.Executor(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list stale trips: %w", err)
	}
	defer rows.Close()

	values := make([]trip.StaleTrip, 0, limit)
	for rows.Next() {
		var value trip.StaleTrip
		if err := rows.Scan(&value.ID, &value.DriverID, &value.LastSeenAt); err != nil {
			return nil, fmt.Errorf("scan stale trip: %w", err)
		}
		value.LastSeenAt = value.LastSeenAt.UTC()
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read stale trips: %w", err)
	}
	return values, nil
}
