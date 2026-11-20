package positions

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/goleak"
	"go.uber.org/mock/gomock"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestScanBatches(t *testing.T) {
	rows := make([]trip.StaleTrip, 250)
	n := byte(1)
	for i := range rows {
		rows[i] = staleTrip(n)
		n++
	}
	for _, tt := range []struct {
		name      string
		queueSize int
		allPages  bool
		read      int
		dropped   int
	}{
		{name: "all pages", queueSize: 250, allPages: true, read: 250},
		{name: "full queue", queueSize: 50, read: 100, dropped: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.QueueSize = tt.queueSize
			repo := NewMockrepository(gomock.NewController(t))
			checkDeadline := func(ctx context.Context, _ time.Duration, _ *trip.StaleTrip, _ int) {
				_, hasDeadline := ctx.Deadline()
				require.True(t, hasDeadline)
			}
			first := repo.EXPECT().ListStale(gomock.Any(), cfg.StaleAfter, nil, cfg.BatchSize).
				Do(checkDeadline).Return(rows[:100], nil)
			if tt.allPages {
				gomock.InOrder(first,
					repo.EXPECT().ListStale(gomock.Any(), cfg.StaleAfter, &rows[99], cfg.BatchSize).
						Do(checkDeadline).Return(rows[100:200], nil),
					repo.EXPECT().ListStale(gomock.Any(), cfg.StaleAfter, &rows[199], cfg.BatchSize).
						Do(checkDeadline).Return(rows[200:], nil),
				)
			}
			pool, stats, _ := testPool(cfg, repo, nil)

			pool.scan(t.Context())

			require.Equal(t, tt.read, receive(t, stats.reads))
			require.Equal(t, tt.queueSize, stats.checks[resultQueued])
			require.Equal(t, tt.dropped, stats.checks[resultDropped])
			require.Len(t, pool.queue, tt.queueSize)
		})
	}
}

func TestQueuePriority(t *testing.T) {
	cfg := testConfig()
	cfg.QueueSize = 3
	pool, _, _ := testPool(cfg, nil, nil)
	fresh := staleTrip(1)
	fresh.LastSeenAt = fresh.LastSeenAt.Add(time.Minute)
	middle := staleTrip(2)
	sameTime := staleTrip(3)
	old := staleTrip(9)
	old.LastSeenAt = old.LastSeenAt.Add(-time.Minute)
	for _, row := range []trip.StaleTrip{fresh, sameTime, middle} {
		result, dropped := pool.enqueue(row, trace.SpanContext{})
		require.Equal(t, resultQueued, result)
		require.Nil(t, dropped)
	}

	result, dropped := pool.enqueue(old, trace.SpanContext{})
	require.Equal(t, resultQueued, result)
	require.Equal(t, &fresh, dropped)
	result, dropped = pool.enqueue(fresh, trace.SpanContext{})
	require.Equal(t, resultDropped, result)
	require.Equal(t, &fresh, dropped)

	pool.closeQueue()
	for _, want := range []trip.StaleTrip{old, middle, sameTime} {
		item := pool.next(t.Context())
		require.NotNil(t, item)
		require.Equal(t, want, item.trip)
	}
	require.Nil(t, pool.next(t.Context()))
}

func TestScanDeduplicatesAndCancels(t *testing.T) {
	first, second := staleTrip(1), staleTrip(2)
	started := make(chan uuid.UUID, 2)
	release := make(chan struct{})
	cfg := testConfig()
	cfg.Workers = 1
	ctrl := gomock.NewController(t)
	repo := NewMockrepository(ctrl)
	push := NewMockpushGateway(ctrl)
	repo.EXPECT().ListStale(gomock.Any(), cfg.StaleAfter, nil, cfg.BatchSize).
		Return([]trip.StaleTrip{first, second}, nil).Times(2)
	push.EXPECT().RequestPosition(gomock.Any(), first.ID, first.DriverID).
		Do(func(ctx context.Context, tripID, _ uuid.UUID) {
			started <- tripID
			select {
			case <-release:
			case <-ctx.Done():
			}
		}).Return(nil)
	pool, stats, clock := testPool(cfg, repo, push)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	startTestPool(t, pool, ctx)

	clock.ticks <- time.Time{}
	require.Equal(t, 2, receive(t, stats.reads))
	require.Equal(t, first.ID, receive(t, started))
	clock.ticks <- time.Time{}
	require.Equal(t, 2, receive(t, stats.reads))
	require.Equal(t, 2, stats.checks[resultQueued])
	require.Equal(t, 2, stats.checks[resultDuplicate])
	pool.Cancel(t.Context(), second.ID)
	require.Equal(t, "cancelled", receive(t, stats.requests))

	cancel()
	receive(t, clock.stopped)
	close(release)
	require.NoError(t, pool.Shutdown(t.Context()))
	require.Empty(t, started)
}

func TestCancelDuringScan(t *testing.T) {
	row := staleTrip(1)
	cfg := testConfig()
	repo := NewMockrepository(gomock.NewController(t))
	pool, stats, _ := testPool(cfg, repo, nil)
	repo.EXPECT().ListStale(gomock.Any(), cfg.StaleAfter, nil, cfg.BatchSize).
		Do(func(ctx context.Context, _ time.Duration, _ *trip.StaleTrip, _ int) {
			pool.Cancel(ctx, row.ID)
		}).Return([]trip.StaleTrip{row}, nil)

	pool.scan(t.Context())

	require.Empty(t, pool.queue)
	require.Empty(t, stats.checks)
	require.Equal(t, 1, receive(t, stats.reads))
}

func TestWorkerSpanLinksToScan(t *testing.T) {
	row := staleTrip(1)
	cfg := testConfig()
	ctrl := gomock.NewController(t)
	repo := NewMockrepository(ctrl)
	push := NewMockpushGateway(ctrl)
	repo.EXPECT().ListStale(gomock.Any(), cfg.StaleAfter, nil, cfg.BatchSize).Return([]trip.StaleTrip{row}, nil)
	push.EXPECT().RequestPosition(gomock.Any(), row.ID, row.DriverID).Return(nil)
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	pool, _, _ := testPool(cfg, repo, push)
	pool.tracer = provider.Tracer("test")
	pool.scan(t.Context())
	pool.closeQueue()
	pool.work(t.Context())

	spans := recorder.Ended()
	require.Len(t, spans, 2)
	require.False(t, spans[1].Parent().IsValid())
	require.Len(t, spans[1].Links(), 1)
	require.Equal(t, spans[0].SpanContext(), spans[1].Links()[0].SpanContext)
}

func TestWorkersDrainAfterPushError(t *testing.T) {
	rows := []trip.StaleTrip{staleTrip(1), staleTrip(2), staleTrip(3), staleTrip(4), staleTrip(5)}
	started := make(chan uuid.UUID, len(rows))
	failFirstPush := make(chan struct{})
	finishOtherPushes := make(chan struct{})
	cfg := testConfig()
	ctrl := gomock.NewController(t)
	repo := NewMockrepository(ctrl)
	push := NewMockpushGateway(ctrl)
	repo.EXPECT().ListStale(gomock.Any(), cfg.StaleAfter, nil, cfg.BatchSize).Return(rows, nil)
	waitForPush := func(ctx context.Context, tripID uuid.UUID, finish <-chan struct{}) {
		started <- tripID
		select {
		case <-finish:
		case <-ctx.Done():
		}
	}
	push.EXPECT().RequestPosition(gomock.Any(), rows[0].ID, rows[0].DriverID).
		Do(func(ctx context.Context, tripID, _ uuid.UUID) {
			waitForPush(ctx, tripID, failFirstPush)
		}).Return(errors.New("push unavailable"))
	for _, row := range rows[1:] {
		push.EXPECT().RequestPosition(gomock.Any(), row.ID, row.DriverID).
			Do(func(ctx context.Context, tripID, _ uuid.UUID) {
				waitForPush(ctx, tripID, finishOtherPushes)
			}).Return(nil)
	}
	pool, stats, clock := testPool(cfg, repo, push)
	startTestPool(t, pool, t.Context())
	clock.ticks <- time.Time{}
	require.Equal(t, len(rows), receive(t, stats.reads))
	first, second := receive(t, started), receive(t, started)
	require.ElementsMatch(t, []uuid.UUID{rows[0].ID, rows[1].ID}, []uuid.UUID{first, second})
	require.EqualValues(t, 2, stats.active.Load())

	stopped := make(chan error, 1)
	go func() { stopped <- pool.Shutdown(t.Context()) }()
	receive(t, clock.stopped)
	close(failFirstPush)
	// The second worker is still blocked, so the first must take the next trip.
	third := receive(t, started)
	require.Equal(t, rows[2].ID, third)
	close(finishOtherPushes)
	require.NoError(t, receive(t, stopped))
	got := []uuid.UUID{first, second, third, receive(t, started), receive(t, started)}
	require.ElementsMatch(t, []uuid.UUID{rows[0].ID, rows[1].ID, rows[2].ID, rows[3].ID, rows[4].ID}, got)
	results := make(map[string]int)
	for range rows {
		results[receive(t, stats.requests)]++
	}
	require.Equal(t, map[string]int{"error": 1, "success": 4}, results)
	require.Zero(t, stats.active.Load())
}

func TestShutdownCancelsPushAtDeadline(t *testing.T) {
	first, second := staleTrip(1), staleTrip(2)
	started := make(chan uuid.UUID, 2)
	finished := make(chan error, 1)
	cfg := testConfig()
	cfg.Workers = 1
	ctrl := gomock.NewController(t)
	repo := NewMockrepository(ctrl)
	push := NewMockpushGateway(ctrl)
	repo.EXPECT().ListStale(gomock.Any(), cfg.StaleAfter, nil, cfg.BatchSize).Return([]trip.StaleTrip{first, second}, nil)
	push.EXPECT().RequestPosition(gomock.Any(), first.ID, first.DriverID).
		Do(func(ctx context.Context, tripID, _ uuid.UUID) {
			started <- tripID
			<-ctx.Done()
			finished <- ctx.Err()
		}).Return(context.Canceled)
	pool, stats, clock := testPool(cfg, repo, push)
	startTestPool(t, pool, t.Context())
	clock.ticks <- time.Time{}
	require.Equal(t, 2, receive(t, stats.reads))
	require.Equal(t, first.ID, receive(t, started))

	shutdownCtx, cancel := context.WithDeadline(t.Context(), time.Unix(0, 0))
	defer cancel()
	require.ErrorIs(t, pool.Shutdown(shutdownCtx), context.DeadlineExceeded)
	require.ErrorIs(t, receive(t, finished), context.Canceled)
	require.Empty(t, started)
	require.Empty(t, pool.queue)
	require.Empty(t, pool.pending)
}

type manualTicker struct {
	ticks   chan time.Time
	stopped chan struct{}
}

func (t manualTicker) Ticks() <-chan time.Time { return t.ticks }
func (t manualTicker) Stop()                   { close(t.stopped) }

type testMetrics struct {
	checks   map[string]int
	reads    chan int
	requests chan string
	active   atomic.Int64
}

func (m *testMetrics) StaleCheck(_ context.Context, result string) { m.checks[result]++ }
func (m *testMetrics) StaleRowsRead(_ context.Context, n int)      { m.reads <- n }
func (m *testMetrics) PositionQueueSize(int)                       {}
func (m *testMetrics) PositionWorkersActive(delta int)             { m.active.Add(int64(delta)) }
func (m *testMetrics) PositionRequest(_ context.Context, result string, _ time.Duration) {
	m.requests <- result
}

func testConfig() Config {
	return Config{
		CheckInterval: 30 * time.Second, StaleAfter: time.Minute, QueryTimeout: time.Second, PushTimeout: time.Minute,
		Workers: 2, QueueSize: 100, BatchSize: 100,
	}
}

func testPool(cfg Config, repo repository, push pushGateway) (*Pool, *testMetrics, manualTicker) {
	stats := &testMetrics{checks: make(map[string]int), reads: make(chan int, 2), requests: make(chan string, 10)}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool := New(cfg, repo, push, stats, logger, noop.NewTracerProvider().Tracer("test"))
	clock := manualTicker{ticks: make(chan time.Time, 1), stopped: make(chan struct{})}
	pool.newTicker = func(time.Duration) ticker { return clock }
	return pool, stats, clock
}

func startTestPool(t *testing.T, pool *Pool, ctx context.Context) {
	t.Helper()
	pool.Start(ctx)
	t.Cleanup(func() { _ = pool.Shutdown(t.Context()) })
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
		var zero T
		return zero
	}
}

func staleTrip(n byte) trip.StaleTrip {
	return trip.StaleTrip{ID: uuid.UUID{15: n}, DriverID: uuid.UUID{14: 1, 15: n}, LastSeenAt: time.Unix(10, 0)}
}
