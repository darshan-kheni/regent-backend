package mime

import (
	"errors"
	"io"
	"log/slog"
	gomime "mime"
	"strings"

	"github.com/emersion/go-message/mail"
	"github.com/google/uuid"
)

// Parse parses a raw RFC 2822 email message into a ParsedEmail.
// Handles multipart/mixed, multipart/alternative, multipart/related.
// CRITICAL: Every part.Body MUST be fully drained before calling NextPart().
func Parse(raw io.Reader, emailID, tenantID, userID uuid.UUID, storage StorageUploader) (*ParsedEmail, error) {
	mr, err := mail.CreateReader(raw)
	if err != nil {
		return nil, errors.Join(errors.New("create mail reader"), err)
	}

	result := &ParsedEmail{}

	// Extract envelope headers
	if date, err := mr.Header.Date(); err == nil {
		result.ReceivedAt = date
	}
	result.Subject = decodeRFC2047(mr.Header.Get("Subject"))
	result.MessageID = strings.Trim(mr.Header.Get("Message-Id"), "<>")
	result.InReplyTo = strings.Trim(mr.Header.Get("In-Reply-To"), "<>")
	result.References = parseReferences(mr.Header.Get("References"))

	// Extract From — include display name when available for extractName()
	if addrs, err := mr.Header.AddressList("From"); err == nil && len(addrs) > 0 {
		if addrs[0].Name != "" {
			result.From = decodeRFC2047(addrs[0].Name) + " <" + addrs[0].Address + ">"
		} else {
			result.From = addrs[0].Address
		}
	}

	// Extract To
	if addrs, err := mr.Header.AddressList("To"); err == nil {
		for _, a := range addrs {
			result.To = append(result.To, a.Address)
		}
	}

	// Extract CC
	if addrs, err := mr.Header.AddressList("Cc"); err == nil {
		for _, a := range addrs {
			result.CC = append(result.CC, a.Address)
		}
	}

	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			slog.Warn("skipping malformed MIME part", "error", err)
			continue
		}

		switch h := part.Header.(type) {
		case *mail.InlineHeader:
			ct, params, _ := h.ContentType()
			charset := params["charset"]
			cid := strings.Trim(h.Get("Content-Id"), "<>")

			switch {
			case ct == "text/plain" && result.TextBody == "":
				body, _ := io.ReadAll(io.LimitReader(part.Body, 1<<20)) // 1MB limit
				result.TextBody = DecodeCharset(string(body), charset)
				// Body is fully consumed by ReadAll

			case ct == "text/html" && result.HTMLBody == "":
				body, _ := io.ReadAll(io.LimitReader(part.Body, 4<<20)) // 4MB limit
				result.HTMLBody = DecodeCharset(string(body), charset)

			case strings.HasPrefix(ct, "image/") && cid != "" && storage != nil:
				// Inline image — upload and track CID mapping
				key, _, err := StreamToStorage(part.Body, storage, tenantID, userID, emailID, cid, ct)
				if err == nil {
					result.InlineImages = append(result.InlineImages, InlineImage{CID: cid, StorageKey: key})
				} else {
					slog.Warn("failed to upload inline image", "cid", cid, "error", err)
					io.Copy(io.Discard, part.Body) // MUST drain
				}

			default:
				io.Copy(io.Discard, part.Body) // MUST drain
			}

		case *mail.AttachmentHeader:
			filename, _ := h.Filename()
			ct, _, _ := h.ContentType()

			if storage == nil {
				io.Copy(io.Discard, part.Body) // Drain if no storage
				continue
			}

			key, size, err := StreamToStorage(part.Body, storage, tenantID, userID, emailID, filename, ct)
			if err != nil {
				slog.Error("uploading attachment", "filename", filename, "error", err)
				io.Copy(io.Discard, part.Body) // Drain on error
				continue
			}

			result.Attachments = append(result.Attachments, Attachment{
				Filename:    filename,
				ContentType: ct,
				Size:        size,
				StorageKey:  key,
			})

		default:
			// Unknown header type — drain
			io.Copy(io.Discard, part.Body)
		}
	}

	return result, nil
}

// decodeRFC2047 decodes MIME encoded-words (e.g. =?UTF-8?B?...?=) in a header value.
func decodeRFC2047(s string) string {
	dec := &gomime.WordDecoder{}
	decoded, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return decoded
}

// parseReferences parses the References header into a list of Message-IDs.
func parseReferences(refs string) []string {
	if refs == "" {
		return nil
	}
	var result []string
	for _, ref := range strings.Fields(refs) {
		ref = strings.Trim(ref, "<>")
		if ref != "" {
			result = append(result, ref)
		}
	}
	return result
}
