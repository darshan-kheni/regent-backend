package gmail

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSignaler struct {
	email     string
	historyID uint64
	called    bool
}

func (m *mockSignaler) SignalGmailSync(email string, historyID uint64) {
	m.email = email
	m.historyID = historyID
	m.called = true
}

func TestPushHandler_ValidNotification(t *testing.T) {
	signaler := &mockSignaler{}
	handler := NewPushHandler(signaler)

	// Build a valid Pub/Sub push notification
	notifData := PushNotification{
		EmailAddress: "user@gmail.com",
		HistoryID:    12345678,
	}
	notifJSON, _ := json.Marshal(notifData)
	encodedData := base64.StdEncoding.EncodeToString(notifJSON)

	envelope := map[string]interface{}{
		"message": map[string]interface{}{
			"data":      encodedData,
			"messageId": "msg-123",
		},
	}
	body, _ := json.Marshal(envelope)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/gmail", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.True(t, signaler.called)
	assert.Equal(t, "user@gmail.com", signaler.email)
	assert.Equal(t, uint64(12345678), signaler.historyID)
}

func TestPushHandler_HistoryIDIsUint64(t *testing.T) {
	signaler := &mockSignaler{}
	handler := NewPushHandler(signaler)

	// Use a large historyId to verify uint64 parsing
	notifData := PushNotification{
		EmailAddress: "user@gmail.com",
		HistoryID:    9999999999999,
	}
	notifJSON, _ := json.Marshal(notifData)
	encodedData := base64.StdEncoding.EncodeToString(notifJSON)

	envelope := map[string]interface{}{
		"message": map[string]interface{}{
			"data":      encodedData,
			"messageId": "msg-456",
		},
	}
	body, _ := json.Marshal(envelope)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/gmail", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, uint64(9999999999999), signaler.historyID)
}

func TestPushHandler_InvalidEnvelope(t *testing.T) {
	handler := NewPushHandler(nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/gmail", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should still return 200 (always acknowledge)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestPushHandler_InvalidBase64(t *testing.T) {
	handler := NewPushHandler(nil)

	envelope := map[string]interface{}{
		"message": map[string]interface{}{
			"data":      "not-valid-base64!!!",
			"messageId": "msg-789",
		},
	}
	body, _ := json.Marshal(envelope)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/gmail", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestPushHandler_NilSignaler(t *testing.T) {
	handler := NewPushHandler(nil)

	notifData := PushNotification{
		EmailAddress: "user@gmail.com",
		HistoryID:    100,
	}
	notifJSON, _ := json.Marshal(notifData)
	encodedData := base64.StdEncoding.EncodeToString(notifJSON)

	envelope := map[string]interface{}{
		"message": map[string]interface{}{
			"data":      encodedData,
			"messageId": "msg-nil",
		},
	}
	body, _ := json.Marshal(envelope)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/gmail", bytes.NewReader(body))
	w := httptest.NewRecorder()

	// Should not panic with nil signaler
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
