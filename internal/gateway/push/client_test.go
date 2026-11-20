package push_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/npapaHAHA/tripgo/internal/gateway/push"
)

func TestRequestPosition(t *testing.T) {
	tripID, driverID := uuid.New(), uuid.New()
	tests := []struct {
		name   string
		status int
	}{
		{"accepted", http.StatusAccepted},
		{"client error", http.StatusBadRequest},
		{"server error", http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/v1/push", r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				var body struct {
					ID          string            `json:"request_id"`
					RecipientID uuid.UUID         `json:"recipient_id"`
					Kind        string            `json:"kind"`
					Data        map[string]string `json:"data"`
				}
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.NotEmpty(t, body.ID)
				assert.Equal(t, driverID, body.RecipientID)
				assert.Equal(t, "REQUEST_POSITION", body.Kind)
				assert.Equal(t, map[string]string{"trip_id": tripID.String()}, body.Data)
				w.WriteHeader(tt.status)
			}))
			defer server.Close()
			client := push.New(server.URL, time.Second, noop.NewTracerProvider())
			defer client.Close()
			err := client.RequestPosition(t.Context(), tripID, driverID)
			if tt.status == http.StatusAccepted {
				require.NoError(t, err)
				return
			}
			var statusErr *push.StatusError
			require.ErrorAs(t, err, &statusErr)
			require.Equal(t, tt.status, statusErr.Code)
		})
	}
}

func TestRequestPositionWithExpiredContext(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client := push.New(server.URL, time.Second, noop.NewTracerProvider())
	defer client.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := client.RequestPosition(ctx, uuid.New(), uuid.New())
	var transportErr *url.Error
	require.ErrorAs(t, err, &transportErr)
	require.ErrorIs(t, err, context.Canceled)
}
