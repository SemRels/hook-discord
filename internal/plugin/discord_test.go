// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 The hook-discord Authors

package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDiscordNotifierNotifySuccess(t *testing.T) {
	var payload WebhookPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	notifier := NewDiscordNotifier(DiscordConfig{WebhookURL: srv.URL})
	err := notifier.Notify(context.Background(), ReleaseNotification{
		Version:                "v1.2.3",
		Changelog:              "- Added a Discord hook",
		ReleaseURL:             "https://github.com/SemRels/hook-discord/releases/tag/v1.2.3",
		Repository:             "SemRels/hook-discord",
		Contributors:           []Contributor{{Name: "Alice", CommitCount: 3, FirstTime: true, FirstContributionLabel: "#42"}, {Name: "Bob", CommitCount: 1}},
		IncludeMVP:             true,
		IncludeNewContributors: true,
		Timestamp:              time.Date(2026, 7, 6, 8, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Len(t, payload.Embeds, 1)
	embed := payload.Embeds[0]
	require.Equal(t, "🚀 Release v1.2.3", embed.Title)
	require.Equal(t, "- Added a Discord hook", embed.Description)
	require.Equal(t, DefaultEmbedColor, embed.Color)
	require.Equal(t, "https://github.com/SemRels/hook-discord/releases/tag/v1.2.3", embed.URL)
	require.Equal(t, "2026-07-06T08:00:00Z", embed.Timestamp)
	require.Len(t, embed.Fields, 4)
	require.Equal(t, "Version", embed.Fields[0].Name)
	require.Equal(t, "SemRels/hook-discord", embed.Fields[1].Value)
	require.Contains(t, embed.Fields[2].Value, "Alice")
	require.Contains(t, embed.Fields[3].Value, "first contribution in #42")
}

func TestDiscordNotifierBuildPayloadWithoutOptionalContributors(t *testing.T) {
	notifier := NewDiscordNotifier(DiscordConfig{WebhookURL: "https://discord.com/api/webhooks/123/secret"})
	payload := notifier.BuildPayload(ReleaseNotification{
		Version:   "v1.0.0",
		Changelog: "- Initial release",
	})

	require.Len(t, payload.Embeds, 1)
	require.Len(t, payload.Embeds[0].Fields, 1)
	require.Equal(t, "Version", payload.Embeds[0].Fields[0].Name)
}

func TestDiscordNotifierNotifyHandlesRateLimitRetry(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"message":"rate limited"}`)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	notifier := NewDiscordNotifier(DiscordConfig{WebhookURL: srv.URL, MaxRetries: 0})
	err := notifier.Notify(context.Background(), ReleaseNotification{Version: "v1.0.0"})
	require.NoError(t, err)
	require.Equal(t, 2, attempts)
}

func TestDiscordNotifierNotifyReturnsNon2xxError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"message":"bad request"}`)
	}))
	defer srv.Close()

	notifier := NewDiscordNotifier(DiscordConfig{WebhookURL: srv.URL})
	err := notifier.Notify(context.Background(), ReleaseNotification{Version: "v1.0.0"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected status 400")
	require.Contains(t, err.Error(), "bad request")
}

func TestDiscordNotifierNotifyRequiresWebhook(t *testing.T) {
	err := NewDiscordNotifier(DiscordConfig{}).Notify(context.Background(), ReleaseNotification{Version: "v1.0.0"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "webhook URL is required")
}

func TestDiscordNotifierTruncatesLongDescription(t *testing.T) {
	notifier := NewDiscordNotifier(DiscordConfig{WebhookURL: "https://discord.com/api/webhooks/123/secret"})
	payload := notifier.BuildPayload(ReleaseNotification{
		Version:   "v9.9.9",
		Changelog: strings.Repeat("x", 3000),
	})

	require.Len(t, []rune(payload.Embeds[0].Description), defaultDescriptionLimit)
	require.True(t, strings.HasSuffix(payload.Embeds[0].Description, "…"))
}

func TestContributorUnmarshalAcceptsSemrelPayload(t *testing.T) {
	var contributors []Contributor
	err := json.Unmarshal([]byte(`[{"name":"Alice","email":"alice@example.com","commits":2,"firstContribution":true}]`), &contributors)
	require.NoError(t, err)
	require.Len(t, contributors, 1)
	require.True(t, contributors[0].FirstTime)
	require.Equal(t, 2, contributors[0].CommitCount)
	require.Equal(t, "Alice", contributors[0].DisplayName())
}
