package send

import (
	"bytes"
	"fmt"
	"strings"
)

// ComposeRequest holds the data needed to compose an email.
type ComposeRequest struct {
	FromAddress  string
	ToAddresses  []string
	CCAddresses  []string
	BCCAddresses []string
	Subject      string
	HTMLBody     string
	// Threading
	InReplyToMessageID string // Original email's message_id
	References         string // Original email's References header value
}

// ComposeMIME builds a MIME message from a compose request.
// Sets In-Reply-To (with angle brackets per RFC 2822) and References for threading.
func ComposeMIME(req *ComposeRequest) ([]byte, error) {
	if req.FromAddress == "" {
		return nil, fmt.Errorf("from address is required")
	}
	if len(req.ToAddresses) == 0 {
		return nil, fmt.Errorf("at least one recipient is required")
	}

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "From: %s\r\n", req.FromAddress)
	fmt.Fprintf(&buf, "To: %s\r\n", strings.Join(req.ToAddresses, ", "))
	if len(req.CCAddresses) > 0 {
		fmt.Fprintf(&buf, "Cc: %s\r\n", strings.Join(req.CCAddresses, ", "))
	}
	fmt.Fprintf(&buf, "Subject: %s\r\n", req.Subject)

	// Threading headers — angle brackets required per RFC 2822
	if req.InReplyToMessageID != "" {
		msgID := req.InReplyToMessageID
		// Ensure angle brackets around message ID
		if !strings.HasPrefix(msgID, "<") {
			msgID = "<" + msgID
		}
		if !strings.HasSuffix(msgID, ">") {
			msgID = msgID + ">"
		}
		fmt.Fprintf(&buf, "In-Reply-To: %s\r\n", msgID)

		// Build References chain
		if req.References != "" {
			fmt.Fprintf(&buf, "References: %s %s\r\n", req.References, msgID)
		} else {
			fmt.Fprintf(&buf, "References: %s\r\n", msgID)
		}
	}

	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: text/html; charset=utf-8\r\n")
	fmt.Fprintf(&buf, "\r\n")
	buf.WriteString(req.HTMLBody)

	return buf.Bytes(), nil
}
