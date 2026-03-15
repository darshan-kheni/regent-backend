package mime

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
)

// StreamToStorage streams attachment content to Supabase Storage via io.Pipe.
// Never buffers files >5MB in memory.
// Storage path: {tenant_id}/{user_id}/{email_id}/{filename}
func StreamToStorage(body io.Reader, storage StorageUploader, tenantID, userID, emailID uuid.UUID, filename string, contentType string) (string, int64, error) {
	key := fmt.Sprintf("%s/%s/%s/%s", tenantID, userID, emailID, filename)

	// Use a counting reader to track size
	cr := &countingReader{r: body}

	storageKey, err := storage.Upload(context.Background(), "attachments", key, cr, contentType)
	if err != nil {
		return "", 0, fmt.Errorf("upload attachment %s: %w", filename, err)
	}

	return storageKey, cr.n, nil
}

// countingReader wraps an io.Reader and counts bytes read.
type countingReader struct {
	r io.Reader
	n int64
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	cr.n += int64(n)
	return n, err
}
