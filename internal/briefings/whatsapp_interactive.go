package briefings

// QuickReplyButton represents a WhatsApp quick reply button.
type QuickReplyButton struct {
	ID    string // Max 256 chars
	Title string // Max 20 chars
}

// DefaultBriefingButtons are the standard quick reply buttons for urgent briefings.
var DefaultBriefingButtons = []QuickReplyButton{
	{ID: "view_email", Title: "View Email"},
	{ID: "dismiss", Title: "Dismiss"},
}

// buildInteractiveQuickReply builds an interactive quick-reply message.
// WhatsApp allows max 3 buttons per message.
func buildInteractiveQuickReply(to, bodyText string, buttons []QuickReplyButton) map[string]interface{} {
	btnList := make([]map[string]interface{}, 0, len(buttons))
	for _, btn := range buttons {
		btnList = append(btnList, map[string]interface{}{
			"type": "reply",
			"reply": map[string]string{
				"id":    btn.ID,
				"title": btn.Title,
			},
		})
	}

	return map[string]interface{}{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "interactive",
		"interactive": map[string]interface{}{
			"type": "button",
			"body": map[string]string{
				"text": bodyText,
			},
			"action": map[string]interface{}{
				"buttons": btnList,
			},
		},
	}
}
