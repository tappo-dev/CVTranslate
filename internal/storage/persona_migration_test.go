package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"cvtranslate/internal/domain"
	"cvtranslate/internal/prompt"
)

func TestPersonaMigrationPreservesTemplatesAndHistory(test *testing.T) {
	databasePath := filepath.Join(test.TempDir(), "persona.sqlite")
	store, err := Open(databasePath)
	if err != nil {
		test.Fatal(err)
	}
	defer store.Close()
	requestContext := context.Background()
	template := prompt.BuiltIn()
	template.ID = "existing-prompt"
	template.BuiltIn = false
	template.Name = "Existing user template"
	template.Revision = 7
	template.Persona = ""
	if err := store.SavePrompt(requestContext, template); err != nil {
		test.Fatal(err)
	}
	configured := template
	configured.ID = "configured-persona"
	configured.Persona = "Keep my custom persona."
	if err := store.SavePrompt(requestContext, configured); err != nil {
		test.Fatal(err)
	}
	settings, err := store.GetSettings(requestContext)
	if err != nil {
		test.Fatal(err)
	}
	settings.DefaultPromptID = template.ID
	if err := store.SaveSettings(requestContext, settings); err != nil {
		test.Fatal(err)
	}
	task := domain.Task{ID: "previous-task", Status: "succeeded", Text: "source", Translation: "result", PromptSnapshot: template, CreatedAt: time.Now().UTC()}
	if err := store.SaveTask(requestContext, task); err != nil {
		test.Fatal(err)
	}
	// Reconstruct pre-persona records only inside this disposable test database.
	for _, statement := range []string{
		"UPDATE prompts SET value=json_remove(value,'$.persona') WHERE id<>'configured-persona'",
		"UPDATE tasks SET value=json_remove(value,'$.promptSnapshot.persona')",
		"DELETE FROM schema_migrations WHERE name='004_prompt_persona.sql'",
		"DELETE FROM schema_migrations WHERE name='005_builtin_general_persona.sql'",
		"DELETE FROM schema_migrations WHERE name='007_builtin_general_code_preservation.sql'",
	} {
		if _, err := store.database.Exec(statement); err != nil {
			test.Fatal(err)
		}
	}
	var originalHistory string
	if err := store.database.QueryRow("SELECT value FROM tasks WHERE id=?", task.ID).Scan(&originalHistory); err != nil {
		test.Fatal(err)
	}
	if err := store.Close(); err != nil {
		test.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		reopened, err := Open(databasePath)
		if err != nil {
			test.Fatal(err)
		}
		defer reopened.Close()
		for _, expected := range []domain.Prompt{prompt.BuiltIn(), template, configured} {
			actual, err := reopened.GetPrompt(requestContext, expected.ID)
			if err != nil || actual != expected {
				test.Fatalf("migration or reopen changed a template: %+v %v", actual, err)
			}
		}
		var personaType string
		if err := reopened.database.QueryRow("SELECT json_type(value,'$.persona') FROM prompts WHERE id=?", template.ID).Scan(&personaType); err != nil || personaType != "text" {
			test.Fatal("migration did not add the empty persona field")
		}
		actualSettings, err := reopened.GetSettings(requestContext)
		if err != nil || actualSettings != settings {
			test.Fatal("migration changed unrelated settings or defaults")
		}
		var currentHistory string
		if err := reopened.database.QueryRow("SELECT value FROM tasks WHERE id=?", task.ID).Scan(&currentHistory); err != nil || currentHistory != originalHistory {
			test.Fatal("migration rewrote historical snapshots")
		}
		storedTask, err := reopened.GetTask(requestContext, task.ID)
		if err != nil || storedTask.PromptSnapshot != template || storedTask.Translation != task.Translation {
			test.Fatal("existing history became unreadable")
		}
		if err := reopened.Close(); err != nil {
			test.Fatal(err)
		}
	}
}
