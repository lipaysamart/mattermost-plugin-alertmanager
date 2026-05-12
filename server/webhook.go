package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/hako/durafmt"
	"github.com/prometheus/alertmanager/notify/webhook"
	"github.com/prometheus/alertmanager/template"

	"github.com/mattermost/mattermost-server/v6/model"
)

func (p *Plugin) handleWebhook(w http.ResponseWriter, r *http.Request, alertConfig alertConfig) {
	p.API.LogInfo("Received alertmanager notification")

	var message webhook.Message
	err := json.NewDecoder(r.Body).Decode(&message)
	if err != nil {
		p.API.LogError("failed to decode webhook message", "err", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if message == (webhook.Message{}) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	receivedAt := time.Now()

	// Check if template is configured
	if alertConfig.hasTemplate() {
		p.handleWebhookWithTemplate(alertConfig, message, receivedAt)
	} else {
		p.handleWebhookDefault(alertConfig, message, receivedAt)
	}
}

// hasTemplate checks if custom template is configured
func (ac *alertConfig) hasTemplate() bool {
	return ac.TitleTemplate != "" || ac.ColorTemplate != "" || ac.FieldsTemplate != ""
}

// handleWebhookWithTemplate processes webhook using custom template
func (p *Plugin) handleWebhookWithTemplate(alertConfig alertConfig, message webhook.Message, receivedAt time.Time) {
	renderer := NewTemplateRenderer()

	// Parse fields template if provided
	var tmplConfig *AlertTemplateConfig
	if alertConfig.FieldsTemplate != "" {
		var fieldsTmpl []FieldTemplate
		if err := json.Unmarshal([]byte(alertConfig.FieldsTemplate), &fieldsTmpl); err != nil {
			p.API.LogError("failed to parse fields template, using default", "err", err.Error())
			p.handleWebhookDefault(alertConfig, message, receivedAt)
			return
		}
		tmplConfig = &AlertTemplateConfig{
			Title:          alertConfig.TitleTemplate,
			Color:          alertConfig.ColorTemplate,
			FieldsTemplate: fieldsTmpl,
		}
	} else {
		tmplConfig = &AlertTemplateConfig{
			Title: alertConfig.TitleTemplate,
			Color: alertConfig.ColorTemplate,
		}
	}

	// Render all alerts
	var fields []*model.SlackAttachmentField
	var lastError error

	for _, alert := range message.Alerts {
		data := &TemplateData{
			Alert:       alert,
			ExternalURL: message.ExternalURL,
			Receiver:    message.Receiver,
			Status:      message.Status,
			ReceivedAt:  receivedAt,
			ConfigID:    alertConfig.ID,
		}

		result, err := renderer.RenderAlert(tmplConfig, data)
		if err != nil {
			p.API.LogError("failed to render template, falling back to default", "err", err.Error())
			lastError = err
			// Fallback to default rendering for this alert
			fields = append(fields, ConvertAlertToFields(alertConfig, alert, message.ExternalURL, message.Receiver)...)
			continue
		}

		// Convert result to fields
		for _, f := range result.Fields {
			fields = append(fields, &model.SlackAttachmentField{
				Title: f.Title,
				Value: f.Value,
				Short: model.SlackCompatibleBool(f.Short),
			})
		}
	}

	// Determine color
	color := setColor(message.Status)
	if alertConfig.ColorTemplate != "" {
		// Render color template for the first alert to get color
		if len(message.Alerts) > 0 {
			data := &TemplateData{
				Alert:       message.Alerts[0],
				ExternalURL: message.ExternalURL,
				Receiver:    message.Receiver,
				Status:      message.Status,
				ReceivedAt:  receivedAt,
				ConfigID:    alertConfig.ID,
			}
			result, err := renderer.RenderAlert(&AlertTemplateConfig{Color: alertConfig.ColorTemplate}, data)
			if err == nil && result.Color != "" {
				color = result.Color
			}
		}
	}

	// Add error message if there were template errors
	if lastError != nil {
		fields = append([]*model.SlackAttachmentField{{
			Title: "Template Error",
			Value: fmt.Sprintf("⚠️ Template rendering failed: %s", lastError.Error()),
			Short: false,
		}}, fields...)
	}

	// Determine title
	title := ""
	if alertConfig.TitleTemplate != "" {
		if len(message.Alerts) > 0 {
			data := &TemplateData{
				Alert:       message.Alerts[0],
				ExternalURL: message.ExternalURL,
				Receiver:    message.Receiver,
				Status:      message.Status,
				ReceivedAt:  receivedAt,
				ConfigID:    alertConfig.ID,
			}
			result, err := renderer.RenderAlert(&AlertTemplateConfig{Title: alertConfig.TitleTemplate}, data)
			if err == nil && result.Title != "" {
				title = result.Title
			}
		}
	}

	attachment := &model.SlackAttachment{
		Title:  title,
		Fields: fields,
		Color:  color,
	}

	post := &model.Post{
		ChannelId: p.AlertConfigIDChannelID[alertConfig.ID],
		UserId:    p.BotUserID,
	}

	model.ParseSlackAttachment(post, []*model.SlackAttachment{attachment})
	if _, appErr := p.API.CreatePost(post); appErr != nil {
		p.API.LogError("failed to create post", "err", appErr.Error())
	}
}

// handleWebhookDefault processes webhook using hardcoded default format
func (p *Plugin) handleWebhookDefault(alertConfig alertConfig, message webhook.Message, receivedAt time.Time) {
	var fields []*model.SlackAttachmentField
	for _, alert := range message.Alerts {
		fields = append(fields, ConvertAlertToFields(alertConfig, alert, message.ExternalURL, message.Receiver)...)
	}

	attachment := &model.SlackAttachment{
		Fields: fields,
		Color:  setColor(message.Status),
	}

	post := &model.Post{
		ChannelId: p.AlertConfigIDChannelID[alertConfig.ID],
		UserId:    p.BotUserID,
	}

	model.ParseSlackAttachment(post, []*model.SlackAttachment{attachment})
	if _, appErr := p.API.CreatePost(post); appErr != nil {
		p.API.LogError("failed to create post", "err", appErr.Error())
	}
}

func addFields(fields []*model.SlackAttachmentField, title, msg string, short bool) []*model.SlackAttachmentField {
	return append(fields, &model.SlackAttachmentField{
		Title: title,
		Value: msg,
		Short: model.SlackCompatibleBool(short),
	})
}

func setColor(impact string) string {
	mapImpactColor := map[string]string{
		"firing":   colorFiring,
		"resolved": colorResolved,
	}

	if val, ok := mapImpactColor[impact]; ok {
		return val
	}

	return colorExpired
}

// ConvertAlertToFields converts an alert to Slack attachment fields (original hardcoded format)
func ConvertAlertToFields(config alertConfig, alert template.Alert, externalURL, receiver string) []*model.SlackAttachmentField {
	var fields []*model.SlackAttachmentField

	statusMsg := strings.ToUpper(alert.Status)
	if alert.Status == "firing" {
		statusMsg = fmt.Sprintf(":fire: %s :fire:", strings.ToUpper(alert.Status))
	}

	/* first field: Annotations, Start/End, Source */
	var msg string
	annotations := make([]string, 0, len(alert.Annotations))
	for k := range alert.Annotations {
		annotations = append(annotations, k)
	}
	sort.Strings(annotations)
	for _, k := range annotations {
		msg = fmt.Sprintf("%s**%s:** %s\n", msg, cases.Title(language.Und, cases.NoLower).String(k), alert.Annotations[k])
	}
	msg = fmt.Sprintf("%s \n", msg)
	msg = fmt.Sprintf("%s**Started at:** %s (%s ago)\n", msg,
		(alert.StartsAt).Format(time.RFC1123),
		durafmt.Parse(time.Since(alert.StartsAt)).LimitFirstN(2).String(),
	)
	if alert.Status == "resolved" {
		msg = fmt.Sprintf("%s**Ended at:** %s (%s ago)\n", msg,
			(alert.EndsAt).Format(time.RFC1123),
			durafmt.Parse(time.Since(alert.EndsAt)).LimitFirstN(2).String(),
		)
	}
	msg = fmt.Sprintf("%s \n", msg)
	msg = fmt.Sprintf("%sGenerated by a [Prometheus Alert](%s) and sent to the [Alertmanager](%s) '%s' receiver.", msg, alert.GeneratorURL, externalURL, receiver)
	fields = addFields(fields, statusMsg, msg, true)

	/* second field: Labels only */
	msg = ""
	alert.Labels["AlertManager Config ID"] = config.ID
	labels := make([]string, 0, len(alert.Labels))
	for k := range alert.Labels {
		labels = append(labels, k)
	}
	sort.Strings(labels)
	for _, k := range labels {
		msg = fmt.Sprintf("%s**%s:** %s\n", msg, cases.Title(language.Und, cases.NoLower).String(k), alert.Labels[k])
	}

	fields = addFields(fields, "", msg, true)

	return fields
}