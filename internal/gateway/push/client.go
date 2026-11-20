package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type Client struct {
	url       string
	http      *http.Client
	transport *http.Transport
}

type StatusError struct {
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("push service returned HTTP %d", e.Code)
}

type request struct {
	ID          string            `json:"request_id"`
	RecipientID uuid.UUID         `json:"recipient_id"`
	Kind        string            `json:"kind"`
	Data        map[string]string `json:"data"`
}

func New(baseURL string, timeout time.Duration, tracerProvider trace.TracerProvider) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &Client{
		url:       strings.TrimRight(baseURL, "/") + "/api/v1/push",
		transport: transport,
		http: &http.Client{
			Timeout: timeout,
			Transport: otelhttp.NewTransport(transport,
				otelhttp.WithTracerProvider(tracerProvider),
				otelhttp.WithPropagators(propagation.TraceContext{}),
				otelhttp.WithSpanNameFormatter(func(string, *http.Request) string { return "push.request_position" }),
			),
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (c *Client) RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error {
	body, err := json.Marshal(request{
		ID:          uuid.NewString(),
		RecipientID: driverID,
		Kind:        "REQUEST_POSITION",
		Data:        map[string]string{"trip_id": tripID.String()},
	})
	if err != nil {
		return fmt.Errorf("encode push request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build push request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("send push request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode != http.StatusAccepted {
		return &StatusError{Code: res.StatusCode}
	}
	return nil
}

func (c *Client) Close() {
	c.transport.CloseIdleConnections()
}
