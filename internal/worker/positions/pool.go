//go:generate go tool mockgen -destination mocks_test.go -package ${GOPACKAGE} . repository,pushGateway

package positions

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
)

type Config struct {
	CheckInterval time.Duration
	StaleAfter    time.Duration
	QueryTimeout  time.Duration
	PushTimeout   time.Duration
	Workers       int
	QueueSize     int
	BatchSize     int
}

type repository interface {
	ListStale(context.Context, time.Duration, *trip.StaleTrip, int) ([]trip.StaleTrip, error)
}

type pushGateway interface {
	RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error
}

type metrics interface {
	StaleCheck(context.Context, string)
	StaleRowsRead(context.Context, int)
	PositionQueueSize(delta int)
	PositionWorkersActive(delta int)
	PositionRequest(context.Context, string, time.Duration)
}

type ticker interface {
	Ticks() <-chan time.Time
	Stop()
}

type clockTicker struct{ *time.Ticker }

func (t clockTicker) Ticks() <-chan time.Time { return t.C }

type Pool struct {
	cfg       Config
	repo      repository
	push      pushGateway
	metrics   metrics
	logger    *slog.Logger
	tracer    trace.Tracer
	newTicker func(time.Duration) ticker

	mu        sync.Mutex
	ready     *sync.Cond
	queue     taskHeap
	pending   map[uuid.UUID]*task
	cancelled map[uuid.UUID]struct{} // Coordinates saved during the current scan.
	closed    bool

	start       sync.Once
	stopScan    context.CancelFunc
	stopWorkers context.CancelFunc
	done        chan struct{}
}

func New(cfg Config, repo repository, push pushGateway, metrics metrics, logger *slog.Logger, tracer trace.Tracer) *Pool {
	p := &Pool{
		cfg: cfg, repo: repo, push: push, metrics: metrics, logger: logger, tracer: tracer,
		newTicker: func(interval time.Duration) ticker { return clockTicker{time.NewTicker(interval)} },
		queue:     make(taskHeap, 0, cfg.QueueSize),
		pending:   make(map[uuid.UUID]*task),
		done:      make(chan struct{}),
	}
	p.ready = sync.NewCond(&p.mu)
	return p
}

func (p *Pool) Start(ctx context.Context) {
	p.start.Do(func() {
		scanCtx, stopScan := context.WithCancel(ctx)
		workerCtx, stopWorkers := context.WithCancel(context.WithoutCancel(ctx))
		p.stopScan = stopScan
		p.stopWorkers = stopWorkers

		var workers sync.WaitGroup
		workers.Add(p.cfg.Workers)
		for range p.cfg.Workers {
			go func() {
				defer workers.Done()
				p.work(workerCtx)
			}()
		}
		go func() {
			defer close(p.done)
			defer stopWorkers()
			defer stopScan()
			p.watch(scanCtx)
			p.closeQueue()
			workers.Wait()
			p.clearQueue()
		}()
	})
}

func (p *Pool) Shutdown(ctx context.Context) error {
	p.start.Do(func() { close(p.done) })
	if p.stopScan == nil {
		return nil
	}
	p.stopScan()
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		p.stopWorkers()
		<-p.done
		return ctx.Err()
	}
}

func (p *Pool) watch(ctx context.Context) {
	clock := p.newTicker(p.cfg.CheckInterval)
	defer clock.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-clock.Ticks():
			p.scan(ctx)
		}
	}
}

func (p *Pool) scan(ctx context.Context) {
	p.mu.Lock()
	p.cancelled = make(map[uuid.UUID]struct{})
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.cancelled = nil
		p.mu.Unlock()
	}()

	ctx, span := p.tracer.Start(ctx, "positions.scan")
	defer span.End()
	rowsRead := 0
	defer func() { p.metrics.StaleRowsRead(ctx, rowsRead) }()

	var after *trip.StaleTrip
	for ctx.Err() == nil {
		queryCtx, cancel := context.WithTimeout(ctx, p.cfg.QueryTimeout)
		rows, err := p.repo.ListStale(queryCtx, p.cfg.StaleAfter, after, p.cfg.BatchSize)
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, "query stale trips")
				p.logger.ErrorContext(ctx, "scan stale trips failed", "error", err)
			}
			return
		}
		rowsRead += len(rows)
		if p.enqueueBatch(ctx, rows, span.SpanContext()) || len(rows) < p.cfg.BatchSize {
			return
		}
		after = &rows[len(rows)-1]
	}
}

func (p *Pool) enqueueBatch(ctx context.Context, rows []trip.StaleTrip, scanSpan trace.SpanContext) bool {
	for _, row := range rows {
		if ctx.Err() != nil {
			return true
		}
		result, dropped := p.enqueue(row, scanSpan)
		if result == "" {
			continue
		}
		p.metrics.StaleCheck(ctx, result)
		if dropped != nil {
			if result != resultDropped {
				p.metrics.StaleCheck(ctx, resultDropped)
			}
			p.logger.WarnContext(ctx, "position queue full", "trip_id", dropped.ID)
			return true
		}
	}
	return false
}

func (p *Pool) work(ctx context.Context) {
	for item := p.next(ctx); item != nil; item = p.next(ctx) {
		p.request(ctx, item)
		p.mu.Lock()
		delete(p.pending, item.trip.ID)
		p.mu.Unlock()
	}
}

func (p *Pool) request(ctx context.Context, item *task) {
	ctx, span := p.tracer.Start(ctx, "positions.request",
		trace.WithNewRoot(), trace.WithLinks(trace.Link{SpanContext: item.scanSpan}),
		trace.WithAttributes(attribute.String("trip.id", item.trip.ID.String())),
	)
	defer span.End()
	ctx, cancel := context.WithTimeout(ctx, p.cfg.PushTimeout)
	defer cancel()
	p.metrics.PositionWorkersActive(1)
	defer p.metrics.PositionWorkersActive(-1)

	start := time.Now()
	err := p.push.RequestPosition(ctx, item.trip.ID, item.trip.DriverID)
	elapsed := time.Since(start)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "request position")
		p.logger.WarnContext(ctx, "request position failed", "trip_id", item.trip.ID, "error", err)
		p.metrics.PositionRequest(ctx, "error", elapsed)
		return
	}
	p.metrics.PositionRequest(ctx, "success", elapsed)
}
