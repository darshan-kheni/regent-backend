package briefings

import "fmt"

// buildUrgentBriefingTemplate builds the regent_urgent_briefing template payload.
// Template has 4 body parameters: sender, subject, summary, action_url
// Plus quick reply buttons: "View Email", "Dismiss"
func buildUrgentBriefingTemplate(to string, b Briefing) map[string]interface{} {
	summary := b.Summary
	if len(summary) > 200 {
		summary = summary[:197] + "..."
	}
	actionURL := b.ActionURL
	if actionURL == "" {
		actionURL = "https://regent.orphilia.com"
	}

	return map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "template",
		"template": map[string]interface{}{
			"name": "regent_urgent_briefing",
			"language": map[string]string{
				"code": "en_US",
			},
			"components": []map[string]interface{}{
				{
					"type": "body",
					"parameters": []map[string]interface{}{
						{"type": "text", "text": b.SenderName},
						{"type": "text", "text": b.Subject},
						{"type": "text", "text": summary},
						{"type": "text", "text": actionURL},
					},
				},
			},
		},
	}
}

// buildDailyDigestTemplate builds the regent_daily_digest template payload.
func buildDailyDigestTemplate(to string, digestCount, urgentCount int) map[string]interface{} {
	return map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "template",
		"template": map[string]interface{}{
			"name": "regent_daily_digest",
			"language": map[string]string{
				"code": "en_US",
			},
			"components": []map[string]interface{}{
				{
					"type": "body",
					"parameters": []map[string]interface{}{
						{"type": "text", "text": fmt.Sprintf("%d", digestCount)},
						{"type": "text", "text": fmt.Sprintf("%d", urgentCount)},
					},
				},
			},
		},
	}
}

// buildReplyConfirmationTemplate builds the regent_reply_confirmation template payload.
func buildReplyConfirmationTemplate(to string, recipientName, subject string) map[string]interface{} {
	return map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "template",
		"template": map[string]interface{}{
			"name": "regent_reply_confirmation",
			"language": map[string]string{
				"code": "en_US",
			},
			"components": []map[string]interface{}{
				{
					"type": "body",
					"parameters": []map[string]interface{}{
						{"type": "text", "text": recipientName},
						{"type": "text", "text": subject},
					},
				},
			},
		},
	}
}
