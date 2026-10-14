package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cvtranslate/internal/domain"
)

func createReuseFixture() domain.Task {
	return domain.Task{
		ID: "original", Text: "Exact source\nwith whitespace  ", Translation: "translated", Status: "succeeded",
		SourceLanguage: "en", TargetLanguage: "zh-CN", CreatedAt: time.Now().UTC(),
		ProviderSnapshot: domain.ProviderConfig{ID: "provider", Type: "openai-compatible", BaseURL: "https://example.invalid/v1", Model: "model"},
		PromptSnapshot:   domain.Prompt{ID: "prompt", SystemTemplate: "Translate to {{target_language}}", UserTemplate: "{{text}}", Persona: "Precise"},
	}
}

func TestReuseRequiresExactInputAndTranslationConfiguration(test *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		test.Fatal(err)
	}
	defer store.Close()
	requestContext := context.Background()
	original := createReuseFixture()
	if err := store.SaveTask(requestContext, original); err != nil {
		test.Fatal(err)
	}
	for _, scenario := range []struct {
		name   string
		change func(*domain.Task)
	}{
		{"text", func(candidate *domain.Task) { candidate.Text += " " }},
		{"source language", func(candidate *domain.Task) { candidate.SourceLanguage = "auto" }},
		{"target language", func(candidate *domain.Task) { candidate.TargetLanguage = "en" }},
		{"provider identity", func(candidate *domain.Task) { candidate.ProviderSnapshot.ID = "other" }},
		{"provider type", func(candidate *domain.Task) { candidate.ProviderSnapshot.Type = "microsoft-translate" }},
		{"endpoint", func(candidate *domain.Task) { candidate.ProviderSnapshot.BaseURL += "/other" }},
		{"model", func(candidate *domain.Task) { candidate.ProviderSnapshot.Model = "other" }},
		{"prompt identity", func(candidate *domain.Task) { candidate.PromptSnapshot.ID = "other" }},
		{"system prompt", func(candidate *domain.Task) { candidate.PromptSnapshot.SystemTemplate += " changed" }},
		{"user prompt", func(candidate *domain.Task) { candidate.PromptSnapshot.UserTemplate += " changed" }},
		{"persona", func(candidate *domain.Task) { candidate.PromptSnapshot.Persona = "Different style" }},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			candidate := original
			scenario.change(&candidate)
			matched, err := store.FindReusableTask(requestContext, candidate)
			if err != nil || matched != nil {
				test.Fatalf("incompatible history matched: %v", err)
			}
		})
	}
	candidate := original
	candidate.Entry = "clipboard-paste"
	candidate.ProviderSnapshot.Name = "Renamed"
	candidate.ProviderSnapshot.TimeoutSeconds = 120
	candidate.PromptSnapshot.Name = "Renamed"
	candidate.PromptSnapshot.Revision++
	matched, err := store.FindReusableTask(requestContext, candidate)
	if err != nil || matched == nil || matched.ID != original.ID {
		test.Fatalf("metadata changes prevented reuse: %v", err)
	}
	if err := store.DeleteTask(requestContext, original.ID); err != nil {
		test.Fatal(err)
	}
	matched, err = store.FindReusableTask(requestContext, candidate)
	if err != nil || matched != nil {
		test.Fatal("deleted history was reused")
	}
}

func TestReuseExcludesUnsuccessfulAndEmptyResults(test *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		test.Fatal(err)
	}
	defer store.Close()
	requestContext := context.Background()
	for _, status := range []string{"queued", "running", "failed", "cancelled", "interrupted", "succeeded"} {
		task := createReuseFixture()
		task.ID = status
		task.Text = status
		task.Status = status
		if status == "succeeded" {
			task.Translation = ""
		}
		if err := store.SaveTask(requestContext, task); err != nil {
			test.Fatal(err)
		}
		matched, err := store.FindReusableTask(requestContext, task)
		if err != nil || matched != nil {
			test.Fatalf("invalid result reused for status %s: %v", status, err)
		}
	}
}

func TestReuseIndexMigrationPreservesAndFindsExistingHistory(test *testing.T) {
	path := filepath.Join(test.TempDir(), "history.sqlite")
	store, err := Open(path)
	if err != nil {
		test.Fatal(err)
	}
	requestContext := context.Background()
	task := createReuseFixture()
	task.PromptSnapshot.Persona = ""
	if err := store.SaveTask(requestContext, task); err != nil {
		test.Fatal(err)
	}
	// Simulate a pre-index database, including history predating optional personas.
	for _, statement := range []string{
		"DROP INDEX tasks_translation_reuse",
		"DELETE FROM schema_migrations WHERE name='006_translation_reuse_index.sql'",
		"UPDATE tasks SET value=json_remove(value,'$.promptSnapshot.persona')",
	} {
		if _, err := store.database.ExecContext(requestContext, statement); err != nil {
			test.Fatal(err)
		}
	}
	var beforeMigration string
	if err := store.database.QueryRowContext(requestContext, "SELECT value FROM tasks WHERE id=?", task.ID).Scan(&beforeMigration); err != nil {
		test.Fatal(err)
	}
	if err := store.Close(); err != nil {
		test.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		test.Fatal(err)
	}
	defer store.Close()
	matched, err := store.FindReusableTask(requestContext, task)
	if err != nil || matched == nil || matched.ID != task.ID {
		test.Fatalf("existing history was not reusable after migration: %v", err)
	}
	var afterMigration string
	if err := store.database.QueryRowContext(requestContext, "SELECT value FROM tasks WHERE id=?", task.ID).Scan(&afterMigration); err != nil {
		test.Fatal(err)
	}
	if beforeMigration != afterMigration {
		test.Fatal("index migration rewrote historical text or snapshots")
	}
}
