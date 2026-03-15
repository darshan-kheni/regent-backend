package connection

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/google/uuid"
)

// ConnectionStatus represents the state of an IMAP connection.
type ConnectionStatus string

const (
	StatusConnected    ConnectionStatus = "connected"
	StatusError        ConnectionStatus = "error"
	StatusReconnecting ConnectionStatus = "reconnecting"
)

// ManagedConnection wraps an IMAP client with metadata.
type ManagedConnection struct {
	AccountID  uuid.UUID
	Client     *imapclient.Client
	Status     ConnectionStatus
	LastFetch  time.Time
	CreatedAt  time.Time
	ErrorCount int
	mu         sync.Mutex
}

// SetStatus updates the connection status thread-safely.
func (mc *ManagedConnection) SetStatus(status ConnectionStatus) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.Status = status
}

// GetStatus returns the current connection status.
func (mc *ManagedConnection) GetStatus() ConnectionStatus {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	return mc.Status
}

// ConnectionManager manages per-account IMAP connections.
// Each email account gets exactly one persistent connection (not pooled/shared).
type ConnectionManager struct {
	mu          sync.RWMutex
	connections map[uuid.UUID]*ManagedConnection
	maxConns    int
}

// NewConnectionManager creates a ConnectionManager with the given max connections.
func NewConnectionManager(maxConns int) *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[uuid.UUID]*ManagedConnection),
		maxConns:    maxConns,
	}
}

// Dial creates a new IMAP connection with TCP keepalive and TLS.
// Uses imapclient.New() with pre-dialed net.Conn for keepalive control.
// DialTLS doesn't expose keepalive, so we dial raw TCP first.
func Dial(ctx context.Context, host string, port int, handler *imapclient.UnilateralDataHandler) (*imapclient.Client, error) {
	addr := fmt.Sprintf("%s:%d", host, port)

	d := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 90 * time.Second,
	}
	rawConn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	tlsConn := tls.Client(rawConn, &tls.Config{ServerName: host})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		rawConn.Close()
		return nil, fmt.Errorf("TLS handshake %s: %w", addr, err)
	}

	opts := &imapclient.Options{
		UnilateralDataHandler: handler,
	}
	client := imapclient.New(tlsConn, opts)

	slog.Debug("IMAP connection established", "host", host, "port", port)
	return client, nil
}

// Register adds a connection to the registry.
func (cm *ConnectionManager) Register(accountID uuid.UUID, client *imapclient.Client) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if len(cm.connections) >= cm.maxConns {
		return fmt.Errorf("connection limit reached: %d/%d", len(cm.connections), cm.maxConns)
	}

	cm.connections[accountID] = &ManagedConnection{
		AccountID: accountID,
		Client:    client,
		Status:    StatusConnected,
		CreatedAt: time.Now(),
	}
	return nil
}

// Remove closes and removes a connection.
func (cm *ConnectionManager) Remove(accountID uuid.UUID) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if conn, ok := cm.connections[accountID]; ok {
		if conn.Client != nil {
			if err := conn.Client.Close(); err != nil {
				slog.Warn("error closing IMAP connection", "account_id", accountID, "error", err)
			}
		}
		delete(cm.connections, accountID)
	}
}

// Get retrieves a connection by account ID.
func (cm *ConnectionManager) Get(accountID uuid.UUID) (*ManagedConnection, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	conn, ok := cm.connections[accountID]
	return conn, ok
}

// Count returns the number of active connections.
func (cm *ConnectionManager) Count() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.connections)
}

// SetError marks a connection as errored.
func (cm *ConnectionManager) SetError(accountID uuid.UUID, err error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if conn, ok := cm.connections[accountID]; ok {
		conn.Status = StatusError
		conn.ErrorCount++
	}
}

// UpdateLastFetch records the most recent successful fetch time.
func (cm *ConnectionManager) UpdateLastFetch(accountID uuid.UUID) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if conn, ok := cm.connections[accountID]; ok {
		conn.LastFetch = time.Now()
	}
}

// CloseAll closes all connections. Used during shutdown.
func (cm *ConnectionManager) CloseAll() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for id, conn := range cm.connections {
		if conn.Client != nil {
			if err := conn.Client.Close(); err != nil {
				slog.Warn("error closing IMAP connection during shutdown", "account_id", id, "error", err)
			}
		}
	}
	cm.connections = make(map[uuid.UUID]*ManagedConnection)
}

// ActiveCount returns the number of connections in connected state.
func (cm *ConnectionManager) ActiveCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	count := 0
	for _, conn := range cm.connections {
		if conn.GetStatus() == StatusConnected {
			count++
		}
	}
	return count
}

// ErrorCount returns the number of connections in error state.
func (cm *ConnectionManager) ErrorCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	count := 0
	for _, conn := range cm.connections {
		if conn.GetStatus() == StatusError {
			count++
		}
	}
	return count
}
