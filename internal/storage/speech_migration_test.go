package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"cvtranslate/internal/domain"
)

func TestSpeechMigrationPreservesLegacySettingsAndHistory(test *testing.T) {
	for _, defaultProvider := range []string{"", "custom"} {
		test.Run("default-"+defaultProvider, func(test *testing.T) {
			path := filepath.Join(test.TempDir(), "legacy.sqlite")
			database, err := sql.Open("sqlite", path)
			if err != nil {
				test.Fatal(err)
			}
			defer database.Close()
			if _, err := database.Exec("CREATE TABLE schema_migrations(name TEXT PRIMARY KEY)"); err != nil {
				test.Fatal(err)
			}
			for _, name := range []string{"001_initial.sql", "002_microsoft_provider.sql"} {
				script, err := migrations.ReadFile("migrations/" + name)
				if err != nil {
					test.Fatal(err)
				}
				if _, err := database.Exec(string(script)); err != nil {
					test.Fatal(err)
				}
				if _, err := database.Exec("INSERT INTO schema_migrations(name) VALUES(?)", name); err != nil {
					test.Fatal(err)
				}
			}
			// Use the exact old JSON contract, not today's DefaultSettings struct.
			legacySettings := `{"defaultProviderId":"` + defaultProvider + `","defaultPromptId":"custom-prompt","sourceLanguage":"en","targetLanguage":"zh-CN","port":19399,"hotkeys":{"openUi":"Ctrl+F8","translatePaste":"Ctrl+C*3","translateUi":"Ctrl+C*4","selectionPaste":"Ctrl+Alt+T","selectionUi":"Ctrl+Alt+Y"},"unrelated":{"preserved":true}}`
			if _, err := database.Exec("INSERT INTO settings(id,value) VALUES(1,?)", legacySettings); err != nil {
				test.Fatal(err)
			}
			legacy := &Store{database: database}
			requestContext := context.Background()
			configuration := domain.ProviderConfig{ID: "custom", Name: "Existing provider", Type: "openai-compatible", BaseURL: "https://example.test", Model: "custom-model", TimeoutSeconds: 17, Enabled: true}
			customPrompt := domain.Prompt{ID: "custom-prompt", Name: "Existing prompt", UserTemplate: "{{text}}", Revision: 3}
			if err := legacy.SaveProvider(requestContext, configuration); err != nil {
				test.Fatal(err)
			}
			if err := legacy.SavePrompt(requestContext, customPrompt); err != nil {
				test.Fatal(err)
			}
			task := domain.Task{ID: "existing-task", Text: "source", Translation: "result", Status: "succeeded", ProviderSnapshot: configuration, PromptSnapshot: customPrompt, CreatedAt: time.Now().UTC()}
			if err := legacy.SaveTask(requestContext, task); err != nil {
				test.Fatal(err)
			}
			var previousHistory string
			if err := database.QueryRow("SELECT value FROM tasks WHERE id=?", task.ID).Scan(&previousHistory); err != nil {
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
			if err != nil || actual.Speech != domain.DefaultSpeechSettings() || actual.DefaultProviderID != defaultProvider {
				test.Fatalf("speech defaults or existing provider choice changed: %+v %v", actual, err)
			}
			var remainingSettings string
			if err := migrated.database.QueryRow("SELECT json_remove(value,'$.speech') FROM settings WHERE id=1").Scan(&remainingSettings); err != nil {
				test.Fatal(err)
			}
			var before, after map[string]any
			if err := json.Unmarshal([]byte(legacySettings), &before); err != nil {
				test.Fatal(err)
			}
			if err := json.Unmarshal([]byte(remainingSettings), &after); err != nil {
				test.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				test.Fatalf("migration changed unrelated settings: %s", remainingSettings)
			}
			storedProvider, err := migrated.GetProvider(requestContext, configuration.ID)
			if err != nil || storedProvider != configuration {
				test.Fatal("migration changed existing provider")
			}
			storedPrompt, err := migrated.GetPrompt(requestContext, customPrompt.ID)
			if err != nil || storedPrompt != customPrompt {
				test.Fatal("migration changed existing prompt")
			}
			var currentHistory string
			if err := migrated.database.QueryRow("SELECT value FROM tasks WHERE id=?", task.ID).Scan(&currentHistory); err != nil || currentHistory != previousHistory {
				test.Fatal("migration changed history")
			}
		})
	}
}

func TestSpeechSettingsDefaultsAndChoicesSurviveReopen(test *testing.T) {
	path := filepath.Join(test.TempDir(), "speech.sqlite")
	store, err := Open(path)
	if err != nil {
		test.Fatal(err)
	}
	defer store.Close()
	requestContext := context.Background()
	settings, err := store.GetSettings(requestContext)
	if err != nil || settings != domain.DefaultSettings() {
		test.Fatalf("fresh defaults missing: %+v %v", settings, err)
	}
	settings.Speech.Engine = "web-speech"
	settings.Speech.EdgeVoices = domain.SpeechVoices{English: "en-US-JennyNeural", Chinese: "zh-CN-YunxiNeural"}
	settings.Speech.WebSpeechVoices = domain.SpeechVoices{English: "browser-english-voice", Chinese: "browser-chinese-voice"}
	settings.DefaultProviderID = ""
	if err := store.SaveSettings(requestContext, settings); err != nil {
		test.Fatal(err)
	}
	if err := store.Close(); err != nil {
		test.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		test.Fatal(err)
	}
	defer reopened.Close()
	actual, err := reopened.GetSettings(requestContext)
	if err != nil || actual != settings {
		test.Fatalf("reopen replaced saved choices: %+v %v", actual, err)
	}
}

func TestSpeechMigrationOnlyAddsAbsentField(test *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		test.Fatal(err)
	}
	defer store.Close()
	script, err := migrations.ReadFile("migrations/003_speech_settings.sql")
	if err != nil {
		test.Fatal(err)
	}
	for _, existingSpeech := range []string{`null`, `{}`, `{"engine":"web-speech","edgeVoices":{"en":"en-US-AriaNeural","zh-CN":"zh-CN-XiaoyiNeural"},"webSpeechVoices":{"en":"chosen","zh-CN":""}}`} {
		original := `{"unrelated":true,"speech":` + existingSpeech + `}`
		if _, err := store.database.Exec("UPDATE settings SET value=? WHERE id=1", original); err != nil {
			test.Fatal(err)
		}
		if _, err := store.database.Exec(string(script)); err != nil {
			test.Fatal(err)
		}
		var actual string
		if err := store.database.QueryRow("SELECT value FROM settings WHERE id=1").Scan(&actual); err != nil || actual != original {
			test.Fatalf("migration replaced a present speech field: %s %v", actual, err)
		}
	}
}
