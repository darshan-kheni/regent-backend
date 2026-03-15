package prompts

import "github.com/darshan-kheni/regent/internal/ai"

// getDefaultTemplate returns the built-in prompt template for a task type.
func getDefaultTemplate(task ai.TaskType) string {
	switch task {
	case ai.TaskCategorize:
		return categorizePriorityTemplate
	case ai.TaskSummarize:
		return summarizeTemplate
	case ai.TaskDraftReply:
		return draftReplyTemplate
	case ai.TaskPremiumDraft:
		return premiumDraftTemplate
	default:
		return categorizePriorityTemplate
	}
}

const categorizePriorityTemplate = `Categorize this email. Output ONLY a JSON object, no other text.

Categories: work, personal, finance, legal, travel, shopping, promotions, newsletters, spam, updates, security, social, recruitment, support, shipping, events, subscriptions, health, education

Rules:
- PRIORITIZE the email BODY CONTENT over the sender name. Read the actual message carefully.
- "personal" = direct messages from a real person expecting a reply (test emails, greetings, follow-ups, questions)
- "work" = direct professional emails about projects, meetings, tasks, deployments, bugs, code reviews
- "promotions" = marketing, sales, dating sites, ads, product launches, discounts, "meet members"
- "spam" = scams, phishing, adult content, unsolicited bulk
- "newsletters" = content digests with articles/links, industry roundups, product update announcements sent to many subscribers
- "updates" = automated service notifications, app updates, account alerts, password resets
- "security" = security alerts, login alerts, 2FA codes, breach notices
- "recruitment" = ONLY real personal recruiter outreach about a specific job
- If someone writes "please let me know", "can you confirm", "please reply" = they expect a reply, so it's "personal" or "work", NOT "newsletters"
- A short direct message from a real person is NEVER a "newsletter" even if the sender name contains words like "outreach" or "team"
- Dating sites and mass marketing are NEVER "work" or "recruitment"

{{if .Personality}}User: {{.Personality}}{{end}}

{"primary_category":"","confidence":0.0,"secondary_category":null,"is_urgent":false,"priority_score":0,"priority_factors":[],"tone":""}`

const summarizeTemplate = `Summarize this email for a busy executive. Output ONLY a JSON object, no other text.

{{if .Personality}}User: {{.Personality}}{{end}}

{"headline":"one-line summary max 100 chars","key_points":["point 1","point 2"],"action_required":false,"action_description":"","urgency_hint":"none"}`

const draftReplyTemplate = `You are drafting an email reply for a busy executive. Read the email carefully and generate the single best reply. Match the tone and formality of the original email. Keep it concise but complete.

{{if .Personality}}Writing style: {{.Personality}}{{end}}

Respond ONLY with valid JSON:
{
  "variants": [
    {"type": "best", "content": "your reply text here", "tone": "matched to original"}
  ]
}`

const premiumDraftTemplate = `You are a highly skilled executive communication specialist. Read the email carefully and generate the single best reply. Match the tone, formality, and communication style of the original email. Be thoughtful about nuance and context.

{{if .Personality}}Writing style and preferences: {{.Personality}}{{end}}

Respond ONLY with valid JSON:
{
  "variants": [
    {"type": "best", "content": "your reply text here", "tone": "matched to original"}
  ],
  "tone_analysis": "brief analysis of appropriate tone for this email"
}`
