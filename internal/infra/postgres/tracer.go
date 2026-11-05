package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type queryMetrics interface {
	DBOperation(ctx context.Context, operation, collection string, elapsed time.Duration, err error)
}

type queryTracer struct {
	inner   pgx.QueryTracer
	metrics queryMetrics
}

type queryKey struct{}

type queryInfo struct {
	started    time.Time
	operation  string
	collection string
}

func (t *queryTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = t.inner.TraceQueryStart(ctx, conn, data)
	operation, collection := queryNames(data.SQL)
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("db.operation.name", operation),
		attribute.String("db.collection.name", collection),
	)
	return context.WithValue(ctx, queryKey{}, queryInfo{time.Now(), operation, collection})
}

func (t *queryTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	info, ok := ctx.Value(queryKey{}).(queryInfo)
	if ok {
		t.metrics.DBOperation(ctx, info.operation, info.collection, time.Since(info.started), data.Err)
	}
	if data.Err != nil {
		span := trace.SpanFromContext(ctx)
		span.SetStatus(codes.Error, "database operation failed")
		span.SetAttributes(attribute.String("error.type", fmt.Sprintf("%T", data.Err)))
	}
	t.inner.TraceQueryEnd(ctx, conn, data)
}

// Queries are built by the repository; only operation and table names become labels.
func queryNames(sql string) (operation, collection string) {
	words := strings.Fields(sql)
	if len(words) == 0 {
		return "UNKNOWN", ""
	}
	operation = strings.ToUpper(words[0])
	for i, word := range words {
		upper := strings.ToUpper(word)
		if (upper == "FROM" || upper == "INTO" || (i == 0 && upper == "UPDATE")) && i+1 < len(words) {
			return operation, strings.Trim(words[i+1], "\"();")
		}
	}
	return operation, ""
}
