// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 The hook-discord Authors

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	plugin "github.com/SemRels/hook-discord/internal/plugin"
	"github.com/stretchr/testify/require"
)

type stubNotifier struct {
	cfg          plugin.DiscordConfig
	notification plugin.ReleaseNotification
	payload      plugin.WebhookPayload
	err          error
	buildCalled  bool
	notifyCalled bool
}

func (s *stubNotifier) BuildPayload(notification plugin.ReleaseNotification) plugin.WebhookPayload {
	s.buildCalled = true
	s.notification = notification
	if len(s.payload.Embeds) > 0 {
		return s.payload
	}
	return plugin.WebhookPayload{
		Embeds: []plugin.Embed{{
			Title:       "🚀 Release " + notification.Version,
			Description: notification.Changelog,
			Color:       plugin.DefaultEmbedColor,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}},
	}
}

func (s *stubNotifier) Notify(_ context.Context, notification plugin.ReleaseNotification) error {
	s.notifyCalled = true
	s.notification = notification
	return s.err
}

func TestRunSuccess(t *testing.T) {
	stub := &stubNotifier{}
	original := newNotifier
	newNotifier = func(cfg plugin.DiscordConfig) notifier {
		stub.cfg = cfg
		return stub
	}
	defer func() { newNotifier = original }()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run(context.Background(), stdout, stderr, env(map[string]string{
		"SEMREL_PLUGIN_DISCORD_WEBHOOK_URL":     "https://discord.com/api/webhooks/123/secret",
		"SEMREL_VERSION":                        "v1.2.3",
		"SEMREL_CHANGELOG":                      "- shipped feature",
		"SEMREL_REPOSITORY_URL":                 "https://github.com/SemRels/semrel",
		"SEMREL_PLUGIN_REPOSITORY":              "SemRels/semrel",
		"SEMREL_PLUGIN_RELEASE_MVP":             "true",
		"SEMREL_PLUGIN_FIRST_TIME_CONTRIBUTORS": "true",
		"SEMREL_CONTRIBUTORS":                   `[{"name":"Alice","commits":3,"firstContribution":true}]`,
	}))

	require.Equal(t, 0, exitCode)
	require.Empty(t, stdout.String())
	require.Equal(t, "plugin_schema_version=1\n", stderr.String())
	require.True(t, stub.notifyCalled)
	require.Equal(t, "v1.2.3", stub.notification.Version)
	require.Equal(t, "https://github.com/SemRels/semrel/releases/tag/v1.2.3", stub.notification.ReleaseURL)
	require.True(t, stub.notification.IncludeMVP)
	require.True(t, stub.notification.IncludeNewContributors)
	require.Len(t, stub.notification.Contributors, 1)
	require.Equal(t, plugin.DefaultMaxRetries, stub.cfg.MaxRetries)
	require.Equal(t, plugin.DefaultRetryDelay, stub.cfg.RetryDelay)
}

func TestRunDryRunPrintsPayloadJSON(t *testing.T) {
	stub := &stubNotifier{payload: plugin.WebhookPayload{Embeds: []plugin.Embed{{Title: "🚀 Release v2.0.0", Description: "- notes", Color: plugin.DefaultEmbedColor}}}}
	original := newNotifier
	newNotifier = func(cfg plugin.DiscordConfig) notifier {
		stub.cfg = cfg
		return stub
	}
	defer func() { newNotifier = original }()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run(context.Background(), stdout, stderr, env(map[string]string{
		"SEMREL_TAG_NAME": "v2.0.0",
		"SEMREL_DRY_RUN":  "true",
	}))

	require.Equal(t, 0, exitCode)
	require.True(t, stub.buildCalled)
	require.False(t, stub.notifyCalled)
	require.Equal(t, "plugin_schema_version=1\n", stderr.String())

	var payload plugin.WebhookPayload
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &payload))
	require.Len(t, payload.Embeds, 1)
	require.Equal(t, "🚀 Release v2.0.0", payload.Embeds[0].Title)
}

func TestRunRequiresWebhookOutsideDryRun(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run(context.Background(), stdout, stderr, env(map[string]string{
		"SEMREL_VERSION": "v1.0.0",
	}))

	require.Equal(t, 1, exitCode)
	require.Contains(t, stderr.String(), "SEMREL_PLUGIN_DISCORD_WEBHOOK_URL is required")
}

func TestRunRequiresVersion(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run(context.Background(), stdout, stderr, env(map[string]string{
		"SEMREL_PLUGIN_DISCORD_WEBHOOK_URL": "https://discord.com/api/webhooks/123/secret",
	}))

	require.Equal(t, 1, exitCode)
	require.Contains(t, stderr.String(), "SEMREL_VERSION, SEMREL_TAG_NAME, or SEMREL_NEXT_VERSION is required")
}

func TestRunNotifierError(t *testing.T) {
	stub := &stubNotifier{err: errors.New("boom")}
	original := newNotifier
	newNotifier = func(cfg plugin.DiscordConfig) notifier { return stub }
	defer func() { newNotifier = original }()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run(context.Background(), stdout, stderr, env(map[string]string{
		"SEMREL_PLUGIN_DISCORD_WEBHOOK_URL": "https://discord.com/api/webhooks/123/secret",
		"SEMREL_NEXT_VERSION":               "v3.0.0",
	}))

	require.Equal(t, 1, exitCode)
	require.Contains(t, stderr.String(), "boom")
}

func TestRunWarnsOnMalformedContributorsJSON(t *testing.T) {
	stub := &stubNotifier{}
	original := newNotifier
	newNotifier = func(cfg plugin.DiscordConfig) notifier { return stub }
	defer func() { newNotifier = original }()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run(context.Background(), stdout, stderr, env(map[string]string{
		"SEMREL_PLUGIN_DISCORD_WEBHOOK_URL": "https://discord.com/api/webhooks/123/secret",
		"SEMREL_VERSION":                    "v1.0.0",
		"SEMREL_CONTRIBUTORS":               `[{`,
	}))

	require.Equal(t, 0, exitCode)
	require.Contains(t, stderr.String(), "invalid SEMREL_CONTRIBUTORS JSON: ignored")
	require.Empty(t, stub.notification.Contributors)
}

func TestReleaseURLFromEnv(t *testing.T) {
	getenv := env(map[string]string{"SEMREL_REPOSITORY_URL": "https://github.com/SemRels/semrel"})
	require.Equal(t, "https://github.com/SemRels/semrel/releases/tag/v1.4.0", releaseURLFromEnv(getenv, "v1.4.0"))
	require.Equal(t, "v1.2.3", firstNonEmpty("", " v1.2.3 "))
}

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}
