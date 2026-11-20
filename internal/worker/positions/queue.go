package positions

import (
	"bytes"
	"container/heap"
	"context"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/npapaHAHA/tripgo/internal/domain/trip"
)

const (
	resultQueued    = "queued"
	resultDuplicate = "duplicate"
	resultDropped   = "dropped"
)

type task struct {
	trip     trip.StaleTrip
	scanSpan trace.SpanContext
	index    int // -1 while a worker is processing the task.
}

type taskHeap []*task

func (h taskHeap) Len() int { return len(h) }

func (h taskHeap) Less(i, j int) bool { return earlier(h[i].trip, h[j].trip) }

func (h taskHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *taskHeap) Push(value any) {
	item := value.(*task)
	item.index = len(*h)
	*h = append(*h, item)
}

func (h *taskHeap) Pop() any {
	i := len(*h) - 1
	item := (*h)[i]
	(*h)[i] = nil
	*h = (*h)[:i]
	item.index = -1
	return item
}

func earlier(a, b trip.StaleTrip) bool {
	if a.LastSeenAt.Equal(b.LastSeenAt) {
		return bytes.Compare(a.ID[:], b.ID[:]) < 0
	}
	return a.LastSeenAt.Before(b.LastSeenAt)
}

func (p *Pool) enqueue(row trip.StaleTrip, scanSpan trace.SpanContext) (string, *trip.StaleTrip) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.cancelled[row.ID]; ok {
		return "", nil
	}
	if _, ok := p.pending[row.ID]; ok {
		return resultDuplicate, nil
	}
	var dropped *trip.StaleTrip
	if len(p.queue) == p.cfg.QueueSize {
		freshest := 0
		for i := 1; i < len(p.queue); i++ {
			if p.queue.Less(freshest, i) {
				freshest = i
			}
		}
		if !earlier(row, p.queue[freshest].trip) {
			return resultDropped, &row
		}
		evicted := heap.Remove(&p.queue, freshest).(*task)
		delete(p.pending, evicted.trip.ID)
		dropped = &evicted.trip
	} else {
		p.metrics.PositionQueueSize(1)
	}
	item := &task{trip: row, scanSpan: scanSpan}
	heap.Push(&p.queue, item)
	p.pending[row.ID] = item
	p.ready.Signal()
	return resultQueued, dropped
}

func (p *Pool) Cancel(ctx context.Context, tripID uuid.UUID) {
	p.mu.Lock()
	if p.cancelled != nil {
		p.cancelled[tripID] = struct{}{}
	}
	item := p.pending[tripID]
	if item == nil || item.index < 0 {
		p.mu.Unlock()
		return
	}
	heap.Remove(&p.queue, item.index)
	delete(p.pending, tripID)
	p.metrics.PositionQueueSize(-1)
	p.mu.Unlock()
	p.metrics.PositionRequest(ctx, "cancelled", 0)
}

func (p *Pool) next(ctx context.Context) *task {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.queue) == 0 && !p.closed {
		p.ready.Wait()
	}
	if len(p.queue) == 0 || ctx.Err() != nil {
		return nil
	}
	p.metrics.PositionQueueSize(-1)
	return heap.Pop(&p.queue).(*task)
}

func (p *Pool) closeQueue() {
	p.mu.Lock()
	p.closed = true
	p.ready.Broadcast()
	p.mu.Unlock()
}

func (p *Pool) clearQueue() {
	p.mu.Lock()
	if len(p.queue) > 0 {
		p.metrics.PositionQueueSize(-len(p.queue))
		clear(p.queue)
		p.queue = p.queue[:0]
		clear(p.pending)
	}
	p.mu.Unlock()
}
