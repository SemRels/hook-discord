// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 The hook-discord Authors

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	plugin "github.com/SemRels/hook-discord/internal/plugin"
)

const pluginSchemaVersion = 1

type notifier interface {
	BuildPayload(plugin.ReleaseNotification) plugin.WebhookPayload
	Notify(context.Context, plugin.ReleaseNotification) error
}

var newNotifier = func(cfg plugin.DiscordConfig) notifier {
	return plugin.NewDiscordNotifier(cfg)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	os.Exit(run(ctx, os.Stdout, os.Stderr, os.Getenv))
}

func run(ctx context.Context, stdout, stderr io.Writer, getenv func(string) string) int {
	_, _ = fmt.Fprintf(stderr, "plugin_schema_version=%d\n", pluginSchemaVersion)

	dryRun := envBool(getenv, "SEMREL_DRY_RUN", false)
	webhookURL := strings.TrimSpace(getenv("SEMREL_PLUGIN_DISCORD_WEBHOOK_URL"))
	if webhookURL == "" && !dryRun {
		_, _ = fmt.Fprintln(stderr, "hook-discord: SEMREL_PLUGIN_DISCORD_WEBHOOK_URL is required")
		return 1
	}

	version := firstNonEmpty(getenv("SEMREL_VERSION"), getenv("SEMREL_TAG_NAME"), getenv("SEMREL_NEXT_VERSION"))
	if version == "" {
		_, _ = fmt.Fprintln(stderr, "hook-discord: SEMREL_VERSION, SEMREL_TAG_NAME, or SEMREL_NEXT_VERSION is required")
		return 1
	}

	maxRetries, err := parseMaxRetries(getenv("SEMREL_PLUGIN_MAX_RETRIES"))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "hook-discord:", err)
		return 1
	}
	retryDelay, err := parseRetryDelay(getenv("SEMREL_PLUGIN_RETRY_DELAY"))
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "hook-discord:", err)
		return 1
	}

	for _, warning := range contributorWarnings(getenv) {
		_, _ = fmt.Fprintln(stderr, "hook-discord:", warning)
	}

	notification := plugin.ReleaseNotification{
		Version:                version,
		Changelog:              getenv("SEMREL_CHANGELOG"),
		ReleaseURL:             releaseURLFromEnv(getenv, version),
		Repository:             strings.TrimSpace(firstNonEmpty(getenv("SEMREL_PLUGIN_REPOSITORY"), getenv("SEMREL_REPOSITORY_URL"))),
		Contributors:           contributorsFromEnv(getenv),
		IncludeNewContributors: envBoolSynonyms(getenv, false, "SEMREL_PLUGIN_FIRST_TIME_CONTRIBUTORS", "SEMREL_PLUGIN_NEW_CONTRIBUTORS"),
		IncludeMVP:             envBoolSynonyms(getenv, false, "SEMREL_PLUGIN_RELEASE_MVP", "SEMREL_PLUGIN_MVP"),
	}

	cfg := plugin.DiscordConfig{
		WebhookURL: webhookURL,
		MaxRetries: maxRetries,
		RetryDelay: retryDelay,
	}

	runner := newNotifier(cfg)
	if dryRun {
		payload := runner.BuildPayload(notification)
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(payload); err != nil {
			_, _ = fmt.Fprintln(stderr, "hook-discord: write dry-run payload:", err)
			return 1
		}
		return 0
	}

	if err := runner.Notify(ctx, notification); err != nil {
		_, _ = fmt.Fprintln(stderr, "hook-discord:", err)
		return 1
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func parseMaxRetries(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return plugin.DefaultMaxRetries, nil
	}
	maxRetries, err := strconv.Atoi(value)
	if err != nil || maxRetries < 0 {
		return 0, fmt.Errorf("SEMREL_PLUGIN_MAX_RETRIES must be a non-negative integer")
	}
	return maxRetries, nil
}

func parseRetryDelay(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return plugin.DefaultRetryDelay, nil
	}
	delay, err := time.ParseDuration(value)
	if err != nil || delay < 0 {
		return 0, fmt.Errorf("SEMREL_PLUGIN_RETRY_DELAY must be a non-negative duration")
	}
	return delay, nil
}

func envBoolSynonyms(getenv func(string) string, defaultValue bool, keys ...string) bool {
	hasExplicit := false
	value := defaultValue
	for _, key := range keys {
		raw := strings.TrimSpace(getenv(key))
		if raw == "" {
			continue
		}
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			continue
		}
		if parsed {
			return true
		}
		hasExplicit = true
		value = false
	}
	if hasExplicit {
		return value
	}
	return defaultValue
}

func envBool(getenv func(string) string, key string, defaultValue bool) bool {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return defaultValue
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func contributorsFromEnv(getenv func(string) string) []plugin.Contributor {
	for _, key := range []string{"SEMREL_CONTRIBUTORS", "SEMREL_PLUGIN_CONTRIBUTORS_JSON"} {
		raw := strings.TrimSpace(getenv(key))
		if raw == "" {
			continue
		}

		var contributors []plugin.Contributor
		if err := json.Unmarshal([]byte(raw), &contributors); err != nil {
			continue
		}
		if key == "SEMREL_PLUGIN_CONTRIBUTORS_JSON" {
			for index := range contributors {
				contributors[index].FirstTime = true
			}
		}
		return contributors
	}
	return nil
}

func contributorWarnings(getenv func(string) string) []string {
	warnings := make([]string, 0, 2)
	for _, key := range []string{"SEMREL_CONTRIBUTORS", "SEMREL_PLUGIN_CONTRIBUTORS_JSON"} {
		raw := strings.TrimSpace(getenv(key))
		if raw == "" {
			continue
		}

		var contributors []plugin.Contributor
		if err := json.Unmarshal([]byte(raw), &contributors); err == nil {
			return warnings
		}
		warnings = append(warnings, fmt.Sprintf("invalid %s JSON: ignored", key))
	}
	return warnings
}

func releaseURLFromEnv(getenv func(string) string, version string) string {
	if explicit := strings.TrimSpace(getenv("SEMREL_PLUGIN_RELEASE_URL")); explicit != "" {
		return explicit
	}

	repositoryURL := strings.TrimSpace(firstNonEmpty(getenv("SEMREL_REPOSITORY_URL"), getenv("SEMREL_PLUGIN_REPOSITORY")))
	if repositoryURL == "" {
		return ""
	}
	if !strings.Contains(repositoryURL, "://") && strings.Count(repositoryURL, "/") == 1 {
		repositoryURL = "https://github.com/" + repositoryURL
	}
	if strings.Contains(repositoryURL, "/releases/") {
		return repositoryURL
	}
	return strings.TrimRight(repositoryURL, "/") + "/releases/tag/" + version
}
