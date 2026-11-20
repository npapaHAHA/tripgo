package telemetry

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type positionMetrics struct {
	saved         metric.Int64Counter
	staleChecks   metric.Int64Counter
	rowsRead      metric.Int64Histogram
	requests      metric.Int64Counter
	duration      metric.Float64Histogram
	queueSize     atomic.Int64
	workersActive atomic.Int64
}

func newPositionMetrics(meter metric.Meter) (*positionMetrics, error) {
	m := &positionMetrics{}
	var err error
	m.saved, err = meter.Int64Counter("tripgo.positions.saved", metric.WithUnit("{position}"))
	if err != nil {
		return nil, fmt.Errorf("create saved positions counter: %w", err)
	}
	m.staleChecks, err = meter.Int64Counter("tripgo.stale_checks", metric.WithUnit("{check}"))
	if err != nil {
		return nil, fmt.Errorf("create stale checks counter: %w", err)
	}
	m.rowsRead, err = meter.Int64Histogram("tripgo.stale_checks.rows_read", metric.WithUnit("{row}"))
	if err != nil {
		return nil, fmt.Errorf("create rows read histogram: %w", err)
	}
	m.requests, err = meter.Int64Counter("tripgo.position_requests", metric.WithUnit("{request}"))
	if err != nil {
		return nil, fmt.Errorf("create position requests counter: %w", err)
	}
	m.duration, err = meter.Float64Histogram("tripgo.position_requests.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, fmt.Errorf("create position request duration histogram: %w", err)
	}
	size, err := meter.Int64ObservableGauge("tripgo.position_requests.queue.size", metric.WithUnit("{job}"))
	if err != nil {
		return nil, fmt.Errorf("create position queue size gauge: %w", err)
	}
	active, err := meter.Int64ObservableGauge("tripgo.position_requests.workers.active", metric.WithUnit("{worker}"))
	if err != nil {
		return nil, fmt.Errorf("create active position workers gauge: %w", err)
	}
	_, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		observer.ObserveInt64(size, m.queueSize.Load())
		observer.ObserveInt64(active, m.workersActive.Load())
		return nil
	}, size, active)
	if err != nil {
		return nil, fmt.Errorf("register position gauges: %w", err)
	}
	return m, nil
}

func (m *positionMetrics) PositionSaved(ctx context.Context) {
	m.saved.Add(ctx, 1)
}

func (m *positionMetrics) StaleCheck(ctx context.Context, result string) {
	m.staleChecks.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}

func (m *positionMetrics) StaleRowsRead(ctx context.Context, count int) {
	m.rowsRead.Record(ctx, int64(count))
}

func (m *positionMetrics) PositionQueueSize(delta int) {
	m.queueSize.Add(int64(delta))
}

func (m *positionMetrics) PositionWorkersActive(delta int) {
	m.workersActive.Add(int64(delta))
}

func (m *positionMetrics) PositionRequest(ctx context.Context, result string, elapsed time.Duration) {
	label := metric.WithAttributes(attribute.String("result", result))
	m.requests.Add(ctx, 1, label)
	if result != "cancelled" {
		m.duration.Record(ctx, elapsed.Seconds(), label)
	}
}
