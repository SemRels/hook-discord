// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 The hook-discord Authors

package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultEmbedColor        = 0x57F287
	defaultDescriptionLimit  = 2048
	defaultFieldValueLimit   = 1024
	defaultResponseBodyLimit = 512
)

type DiscordConfig struct {
	WebhookURL string
	MaxRetries int
	RetryDelay time.Duration
	EmbedColor int
}

type ReleaseNotification struct {
	Version                string
	Changelog              string
	ReleaseURL             string
	Repository             string
	Contributors           []Contributor
	IncludeNewContributors bool
	IncludeMVP             bool
	Timestamp              time.Time
}

type DiscordNotifier struct {
	cfg    DiscordConfig
	client *http.Client
}

type WebhookPayload struct {
	Embeds []Embed `json:"embeds"`
}

type Embed struct {
	Title       string  `json:"title,omitempty"`
	Description string  `json:"description,omitempty"`
	Color       int     `json:"color,omitempty"`
	Fields      []Field `json:"fields,omitempty"`
	Timestamp   string  `json:"timestamp,omitempty"`
	URL         string  `json:"url,omitempty"`
}

type Field struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

func NewDiscordNotifier(cfg DiscordConfig) *DiscordNotifier {
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = DefaultMaxRetries
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = DefaultRetryDelay
	}
	if cfg.EmbedColor == 0 {
		cfg.EmbedColor = DefaultEmbedColor
	}
	return &DiscordNotifier{
		cfg:    cfg,
		client: &http.Client{Timeout: DefaultTimeout},
	}
}

func (n *DiscordNotifier) BuildPayload(release ReleaseNotification) WebhookPayload {
	timestamp := release.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}

	embed := Embed{
		Title:       fmt.Sprintf("🚀 Release %s", release.Version),
		Description: truncateText(release.Changelog, defaultDescriptionLimit, "…"),
		Color:       n.cfg.EmbedColor,
		Timestamp:   timestamp.UTC().Format(time.RFC3339),
		URL:         strings.TrimSpace(release.ReleaseURL),
		Fields:      buildFields(release),
	}
	return WebhookPayload{Embeds: []Embed{embed}}
}

func (n *DiscordNotifier) Notify(ctx context.Context, release ReleaseNotification) error {
	if strings.TrimSpace(n.cfg.WebhookURL) == "" {
		return fmt.Errorf("discord: webhook URL is required")
	}
	if _, err := url.ParseRequestURI(n.cfg.WebhookURL); err != nil {
		return fmt.Errorf("discord: invalid webhook URL")
	}

	payload := n.BuildPayload(release)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("discord: marshal payload: %w", err)
	}

	rateLimitRetried := false
	resp, err := retryDo(ctx, n.cfg.MaxRetries, n.cfg.RetryDelay, func() (*http.Response, error) {
		return n.doRequest(ctx, body, &rateLimitRetried)
	})
	if err != nil {
		return fmt.Errorf("discord: send notification: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, defaultResponseBodyLimit))
		if len(responseBody) == 0 {
			return fmt.Errorf("discord: unexpected status %d", resp.StatusCode)
		}
		return fmt.Errorf("discord: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func (n *DiscordNotifier) doRequest(ctx context.Context, body []byte, rateLimitRetried *bool) (*http.Response, error) {
	resp, err := n.post(ctx, body)
	if err != nil || resp == nil || resp.StatusCode != http.StatusTooManyRequests || *rateLimitRetried {
		return resp, err
	}

	*rateLimitRetried = true
	wait := retryAfterDelay(resp.Header.Get("Retry-After"))
	_, _ = fmt.Fprintf(retryLogWriter, "retry attempt 1/1 after %s: unexpected status 429\n", wait)
	_ = resp.Body.Close()
	if err := sleepContext(ctx, wait); err != nil {
		return nil, err
	}
	return n.post(ctx, body)
}

func (n *DiscordNotifier) post(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("discord: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return n.client.Do(req)
}

func buildFields(release ReleaseNotification) []Field {
	fields := []Field{{
		Name:   "Version",
		Value:  truncateText(strings.TrimSpace(release.Version), defaultFieldValueLimit, "…"),
		Inline: true,
	}}
	if repository := strings.TrimSpace(release.Repository); repository != "" {
		fields = append(fields, Field{Name: "Repository", Value: truncateText(repository, defaultFieldValueLimit, "…"), Inline: true})
	}
	if release.IncludeMVP {
		if mvp, ok := topContributor(release.Contributors); ok {
			fields = append(fields, Field{
				Name:  "🏆 Release MVP",
				Value: truncateText(formatMVP(mvp), defaultFieldValueLimit, "…"),
			})
		}
	}
	if release.IncludeNewContributors {
		if summary := formatNewContributors(release.Contributors); summary != "" {
			fields = append(fields, Field{
				Name:  "New Contributors",
				Value: truncateText(summary, defaultFieldValueLimit, "…"),
			})
		}
	}
	return fields
}

func topContributor(contributors []Contributor) (Contributor, bool) {
	var best Contributor
	bestCount := -1
	for _, contributor := range contributors {
		if contributor.CommitCount > bestCount && strings.TrimSpace(contributor.DisplayName()) != "" {
			best = contributor
			bestCount = contributor.CommitCount
		}
	}
	if bestCount < 0 {
		return Contributor{}, false
	}
	return best, true
}

func formatMVP(contributor Contributor) string {
	count := contributor.CommitCount
	if count == 1 {
		return fmt.Sprintf("%s led this release with 1 commit.", contributor.DisplayName())
	}
	if count > 1 {
		return fmt.Sprintf("%s led this release with %d commits.", contributor.DisplayName(), count)
	}
	return fmt.Sprintf("%s led this release.", contributor.DisplayName())
}

func formatNewContributors(contributors []Contributor) string {
	lines := make([]string, 0, len(contributors))
	for _, contributor := range contributors {
		if !contributor.FirstTime {
			continue
		}
		line := contributor.DisplayName()
		if contributor.FirstContributionLabel != "" {
			line += fmt.Sprintf(" — first contribution in %s", contributor.FirstContributionLabel)
		}
		lines = append(lines, "• "+line)
	}
	return strings.Join(lines, "\n")
}

func truncateText(value string, limit int, suffix string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Release completed successfully."
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	suffixRunes := []rune(suffix)
	if limit <= len(suffixRunes) {
		return string(runes[:limit])
	}
	return string(runes[:limit-len(suffixRunes)]) + suffix
}

func retryAfterDelay(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds >= 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	if when, err := http.ParseTime(value); err == nil {
		delay := time.Until(when)
		if delay > 0 {
			return delay
		}
	}
	return 0
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
