package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"cvtranslate/internal/domain"
)

func TestMicrosoftMigrationPreservesExistingData(test *testing.T) {
	for _, defaultProvider := range []string{"", "custom"} {
		test.Run("default-"+defaultProvider, func(test *testing.T) {
			path := filepath.Join(test.TempDir(), "existing.sqlite")
			database, err := sql.Open("sqlite", path)
			if err != nil {
				test.Fatal(err)
			}
			initial, err := migrations.ReadFile("migrations/001_initial.sql")
			if err != nil {
				test.Fatal(err)
			}
			if _, err := database.Exec(string(initial)); err != nil {
				test.Fatal(err)
			}
			if _, err := database.Exec("CREATE TABLE schema_migrations(name TEXT PRIMARY KEY); INSERT INTO schema_migrations VALUES('001_initial.sql')"); err != nil {
				test.Fatal(err)
			}
			legacy := &Store{database: database}
			requestContext := context.Background()
			settings := domain.DefaultSettings()
			settings.DefaultProviderID = defaultProvider
			settings.Hotkeys.OpenUI = "Ctrl+F8"
			configuration := domain.ProviderConfig{ID: "custom", Name: "Existing", Type: "openai-compatible", BaseURL: "https://example.test", Model: "existing", TimeoutSeconds: 17, Enabled: true}
			if err := legacy.SaveSettings(requestContext, settings); err != nil {
				test.Fatal(err)
			}
			if err := legacy.SaveProvider(requestContext, configuration); err != nil {
				test.Fatal(err)
			}
			customPrompt := domain.Prompt{ID: "custom-prompt", Name: "Existing prompt", UserTemplate: "{{text}}", Revision: 3}
			if err := legacy.SavePrompt(requestContext, customPrompt); err != nil {
				test.Fatal(err)
			}
			task := domain.Task{ID: "existing-task", Text: "source", Translation: "result", Status: "succeeded", ProviderSnapshot: configuration, PromptSnapshot: customPrompt, CreatedAt: time.Now().UTC()}
			if err := legacy.SaveTask(requestContext, task); err != nil {
				test.Fatal(err)
			}
			var before string
			if err := database.QueryRow("SELECT value FROM tasks WHERE id=?", task.ID).Scan(&before); err != nil {
				test.Fatal(err)
			}
			if err := legacy.Close(); err != nil {
				test.Fatal(err)
			}
			migrated, err := Open(path)
			if err != nil {
				test.Fatal(err)
			}
			defer migrated.Close()
			actual, err := migrated.GetSettings(requestContext)
			if defaultProvider == "" {
				settings.DefaultProviderID = domain.BuiltInMicrosoftProviderID
			}
			if err != nil || actual != settings {
				test.Fatalf("settings changed: %+v %v", actual, err)
			}
			stored, err := migrated.GetProvider(requestContext, configuration.ID)
			if err != nil || stored != configuration {
				test.Fatal("existing provider changed")
			}
			storedPrompt, err := migrated.GetPrompt(requestContext, customPrompt.ID)
			if err != nil || storedPrompt != customPrompt {
				test.Fatal("existing prompt changed")
			}
			var after string
			if err := migrated.database.QueryRow("SELECT value FROM tasks WHERE id=?", task.ID).Scan(&after); err != nil || before != after {
				test.Fatal("snapshot changed")
			}
			builtin, err := migrated.GetProvider(requestContext, domain.BuiltInMicrosoftProviderID)
			if err != nil || builtin.Name != "Microsoft Translator" || builtin.Type != "microsoft-translate" || builtin.BaseURL != "" || builtin.Model != "" || builtin.TimeoutSeconds != 30 || !builtin.Enabled {
				test.Fatalf("invalid seed: %+v %v", builtin, err)
			}
		})
	}
}

func TestMicrosoftSeedReopenAndDeletion(test *testing.T) {
	path := filepath.Join(test.TempDir(), "new.sqlite")
	store, err := Open(path)
	if err != nil {
		test.Fatal(err)
	}
	requestContext := context.Background()
	settings, err := store.GetSettings(requestContext)
	if err != nil || settings.DefaultProviderID != domain.BuiltInMicrosoftProviderID {
		test.Fatal("new installation default missing")
	}
	builtin, err := store.GetProvider(requestContext, domain.BuiltInMicrosoftProviderID)
	if err != nil {
		test.Fatal(err)
	}
	builtin.Name = "User edited name"
	if err := store.SaveProvider(requestContext, builtin); err != nil {
		test.Fatal(err)
	}
	if err := store.Close(); err != nil {
		test.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		test.Fatal(err)
	}
	stored, err := store.GetProvider(requestContext, builtin.ID)
	if err != nil || stored != builtin {
		test.Fatal("reopening replaced user configuration")
	}
	settings.DefaultProviderID = ""
	if err := store.SaveSettings(requestContext, settings); err != nil {
		test.Fatal(err)
	}
	if err := store.DeleteProvider(requestContext, builtin.ID); err != nil {
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
	if _, err := store.GetProvider(requestContext, builtin.ID); !errors.Is(err, ErrNotFound) {
		test.Fatal("deleted provider recreated")
	}
	reopened, err := store.GetSettings(requestContext)
	if err != nil || reopened != settings {
		test.Fatal("cleared default changed on reopen")
	}
	var migrationCount int
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		test.Fatal(err)
	}
	if err := store.database.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&migrationCount); err != nil || migrationCount != len(entries) {
		test.Fatal("migration not idempotent")
	}
}

func TestMicrosoftMigrationDoesNotReplaceCollidingProvider(test *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		test.Fatal(err)
	}
	defer store.Close()
	configuration := domain.ProviderConfig{ID: domain.BuiltInMicrosoftProviderID, Name: "Preserve this row", Type: "openai-compatible", Model: "existing"}
	encoded, err := json.Marshal(configuration)
	if err != nil {
		test.Fatal(err)
	}
	if _, err := store.database.Exec("UPDATE providers SET value=? WHERE id=?", string(encoded), configuration.ID); err != nil {
		test.Fatal(err)
	}
	script, err := migrations.ReadFile("migrations/002_microsoft_provider.sql")
	if err != nil {
		test.Fatal(err)
	}
	if _, err := store.database.Exec(string(script)); err != nil {
		test.Fatal(err)
	}
	actual, err := store.GetProvider(context.Background(), configuration.ID)
	if err != nil || actual != configuration {
		test.Fatal("existing ID overwritten")
	}
}
