package briefings

import (
	"bytes"
	"fmt"
	"html/template"
	"time"
)

// DigestHTMLRenderer renders digest data into HTML email with inline CSS.
// Uses table layout for Outlook compatibility. Inline CSS only (Gmail strips <style>).
// Typography: EB Garamond headings (fallback Georgia), Inter body (fallback Arial).
// Colors: dark background (#0A0A08), gold accents (#C9A96E). Max 600px width.
type DigestHTMLRenderer struct {
	tmpl *template.Template
}

// NewDigestHTMLRenderer creates a renderer with the pre-compiled template.
func NewDigestHTMLRenderer() *DigestHTMLRenderer {
	funcMap := template.FuncMap{
		"formatTime": func(t time.Time) string {
			return t.Format("3:04 PM")
		},
		"formatDate": func(t time.Time) string {
			return t.Format("Monday, January 2, 2006")
		},
		"truncate": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			if n <= 3 {
				return s[:n]
			}
			return s[:n-3] + "..."
		},
	}

	tmpl := template.Must(template.New("digest").Funcs(funcMap).Parse(digestTemplate))
	return &DigestHTMLRenderer{tmpl: tmpl}
}

// Render produces the HTML email body. Returns error if result exceeds 100KB.
func (r *DigestHTMLRenderer) Render(data *DigestData) (string, error) {
	var buf bytes.Buffer
	if err := r.tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("digest render: %w", err)
	}

	html := buf.String()
	if len(html) > 100*1024 {
		return "", fmt.Errorf("digest render: HTML exceeds 100KB limit (%d bytes)", len(html))
	}

	return html, nil
}

const digestTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Regent Daily Briefing</title>
</head>
<body style="margin:0;padding:0;background-color:#0A0A08;font-family:Inter,Arial,Helvetica,sans-serif;">
<table role="presentation" cellpadding="0" cellspacing="0" width="100%" style="background-color:#0A0A08;">
<tr><td align="center" style="padding:20px 0;">
<table role="presentation" cellpadding="0" cellspacing="0" width="600" style="max-width:600px;width:100%;">

<!-- Header -->
<tr><td style="padding:30px 24px 20px;border-bottom:2px solid #C9A96E;">
<table role="presentation" cellpadding="0" cellspacing="0" width="100%">
<tr>
<td style="font-family:'EB Garamond',Georgia,serif;font-size:28px;color:#C9A96E;letter-spacing:2px;">REGENT</td>
<td align="right" style="font-family:Inter,Arial,sans-serif;font-size:13px;color:#888;">{{formatDate .PeriodEnd}}</td>
</tr>
</table>
</td></tr>

<!-- Summary Bar -->
<tr><td style="padding:16px 24px;background-color:#111;">
<table role="presentation" cellpadding="0" cellspacing="0" width="100%">
<tr>
<td style="font-family:Inter,Arial,sans-serif;font-size:13px;color:#aaa;">
{{.TotalCount}} emails processed
{{if gt .UrgentCount 0}} &bull; <span style="color:#E53935;">{{.UrgentCount}} urgent</span>{{end}}
{{if gt .NeedsReplyCount 0}} &bull; <span style="color:#C9A96E;">{{.NeedsReplyCount}} need reply</span>{{end}}
</td>
</tr>
</table>
</td></tr>

{{if .Urgent}}
<!-- Urgent Section -->
<tr><td style="padding:20px 24px 8px;">
<table role="presentation" cellpadding="0" cellspacing="0" width="100%">
<tr><td style="font-family:'EB Garamond',Georgia,serif;font-size:18px;color:#E53935;padding-bottom:8px;border-bottom:1px solid #333;">
URGENT
</td></tr>
</table>
</td></tr>
{{range .Urgent}}
<tr><td style="padding:8px 24px;">
<table role="presentation" cellpadding="0" cellspacing="0" width="100%" style="border-left:3px solid #E53935;padding-left:12px;">
<tr><td>
<div style="font-family:Inter,Arial,sans-serif;font-size:14px;color:#fff;font-weight:600;">{{.SenderName}}</div>
<div style="font-family:Inter,Arial,sans-serif;font-size:14px;color:#ddd;">{{truncate .Subject 80}}</div>
<div style="font-family:Inter,Arial,sans-serif;font-size:12px;color:#999;margin-top:4px;">{{truncate .Summary 150}}</div>
</td>
<td align="right" valign="top" style="font-family:Inter,Arial,sans-serif;font-size:11px;color:#666;white-space:nowrap;">{{formatTime .ReceivedAt}}</td>
</tr>
</table>
</td></tr>
{{end}}
{{end}}

{{if .NeedsReply}}
<!-- Needs Reply Section -->
<tr><td style="padding:20px 24px 8px;">
<table role="presentation" cellpadding="0" cellspacing="0" width="100%">
<tr><td style="font-family:'EB Garamond',Georgia,serif;font-size:18px;color:#C9A96E;padding-bottom:8px;border-bottom:1px solid #333;">
NEEDS REPLY
</td></tr>
</table>
</td></tr>
{{range .NeedsReply}}
<tr><td style="padding:8px 24px;">
<table role="presentation" cellpadding="0" cellspacing="0" width="100%" style="border-left:3px solid #C9A96E;padding-left:12px;">
<tr><td>
<div style="font-family:Inter,Arial,sans-serif;font-size:14px;color:#fff;font-weight:600;">{{.SenderName}}</div>
<div style="font-family:Inter,Arial,sans-serif;font-size:14px;color:#ddd;">{{truncate .Subject 80}}</div>
<div style="font-family:Inter,Arial,sans-serif;font-size:12px;color:#999;margin-top:4px;">{{truncate .Summary 150}}</div>
</td>
<td align="right" valign="top" style="font-family:Inter,Arial,sans-serif;font-size:11px;color:#666;white-space:nowrap;">{{formatTime .ReceivedAt}}</td>
</tr>
</table>
</td></tr>
{{end}}
{{end}}

{{if .FYI}}
<!-- FYI Section -->
<tr><td style="padding:20px 24px 8px;">
<table role="presentation" cellpadding="0" cellspacing="0" width="100%">
<tr><td style="font-family:'EB Garamond',Georgia,serif;font-size:18px;color:#888;padding-bottom:8px;border-bottom:1px solid #333;">
FYI
</td></tr>
</table>
</td></tr>
{{range .FYI}}
<tr><td style="padding:8px 24px;">
<table role="presentation" cellpadding="0" cellspacing="0" width="100%" style="border-left:3px solid #444;padding-left:12px;">
<tr><td>
<div style="font-family:Inter,Arial,sans-serif;font-size:14px;color:#ccc;">{{.SenderName}} — {{truncate .Subject 60}}</div>
<div style="font-family:Inter,Arial,sans-serif;font-size:12px;color:#777;margin-top:2px;">{{truncate .Summary 120}}</div>
</td>
<td align="right" valign="top" style="font-family:Inter,Arial,sans-serif;font-size:11px;color:#555;white-space:nowrap;">{{formatTime .ReceivedAt}}</td>
</tr>
</table>
</td></tr>
{{end}}
{{end}}

<!-- Footer -->
<tr><td style="padding:24px;border-top:1px solid #333;margin-top:16px;">
<table role="presentation" cellpadding="0" cellspacing="0" width="100%">
<tr><td style="font-family:Inter,Arial,sans-serif;font-size:11px;color:#555;text-align:center;">
Regent AI Executive Assistant &bull; <a href="https://regent.orphilia.com/settings/briefings" style="color:#C9A96E;text-decoration:none;">Manage Digest Settings</a>
</td></tr>
</table>
</td></tr>

</table>
</td></tr>
</table>
</body>
</html>`
