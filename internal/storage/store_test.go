package storage

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cvtranslate/internal/domain"
)

func TestMigrationRecoveryAndLiteralSearch(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.sqlite")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := store.GetSettings(ctx)
	if err != nil || settings.DefaultPromptID != "builtin-general" {
		t.Fatalf("seed: %v %v", settings, err)
	}
	for index, text := range []string{"100%_\\ literal", "unrelated"} {
		status := "succeeded"
		if index == 0 {
			status = "running"
		}
		task := domain.Task{ID: domain.NewID(), Status: status, Text: text, CreatedAt: time.Now().UTC()}
		if err := store.SaveTask(ctx, task); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			if !errors.Is(store.DeleteTask(ctx, task.ID), ErrConflict) || !errors.Is(store.ClearTasks(ctx), ErrConflict) {
				t.Fatal("active deletion allowed")
			}
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	page, err := store.ListTasks(ctx, domain.TaskFilter{Query: "%_\\", PageSize: 999})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.PageSize != 100 || page.Items[0].Status != "interrupted" {
		t.Fatalf("unexpected page: %+v", page)
	}
	if err := store.SaveTask(ctx, page.Items[0]); !errors.Is(err, ErrConflict) {
		t.Fatalf("terminal overwrite allowed: %v", err)
	}
	if err := store.UpdatePasteStatus(ctx, page.Items[0].ID, "manual_copy"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeletePrompt(ctx, "builtin-general"); !errors.Is(err, ErrConflict) {
		t.Fatal("builtin deleted")
	}
	if err := store.ClearTasks(ctx); err != nil {
		t.Fatal(err)
	}
	page, err = store.ListTasks(ctx, domain.TaskFilter{})
	if err != nil || page.Total != 0 {
		t.Fatalf("clear failed: %v", err)
	}
	if _, err := store.GetTask(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing task error")
	}
}

func TestListTasksFiltersByCreatedTimeRange(test *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		test.Fatal(err)
	}
	defer store.Close()
	requestContext := context.Background()
	for _, task := range []domain.Task{
		{ID: "before-range", Status: "succeeded", Text: "before", CreatedAt: time.Date(2026, 10, 13, 23, 59, 59, 0, time.UTC)},
		{ID: "inside-range", Status: "succeeded", Text: "inside", CreatedAt: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)},
		{ID: "after-range", Status: "succeeded", Text: "after", CreatedAt: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)},
	} {
		if err := store.SaveTask(requestContext, task); err != nil {
			test.Fatal(err)
		}
	}

	page, err := store.ListTasks(requestContext, domain.TaskFilter{
		CreatedAfter:  time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
		CreatedBefore: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		test.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "inside-range" {
		test.Fatalf("unexpected date-filtered page: %+v", page)
	}
	if _, err := store.ListTasks(requestContext, domain.TaskFilter{
		CreatedAfter:  time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC),
		CreatedBefore: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
	}); err == nil {
		test.Fatal("invalid date range was accepted")
	}
}

func TestRemovedTranslationHotkeysDoNotRemainInSettingsContract(test *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		test.Fatal(err)
	}
	defer store.Close()
	requestContext := context.Background()
	if _, err := store.database.ExecContext(requestContext, `UPDATE settings SET value=json_set(value, '$.hotkeys', json(?)) WHERE id=1`, `{"openUi":"Ctrl+Space","translatePaste":"Ctrl+Alt+T","translateUi":"Ctrl+Alt+U","selectionPaste":"Ctrl+Alt+Y","selectionUi":"Ctrl+Alt+I"}`); err != nil {
		test.Fatal(err)
	}
	settings, err := store.GetSettings(requestContext)
	if err != nil {
		test.Fatal(err)
	}
	expected := domain.Hotkeys{OpenUI: "Ctrl+Space"}
	if settings.Hotkeys != expected {
		test.Fatal("removed translation bindings escaped the current settings contract")
	}
	if err := store.SaveSettings(requestContext, settings); err != nil {
		test.Fatal(err)
	}
	saved, err := store.GetSettings(requestContext)
	if err != nil || saved != settings {
		test.Fatalf("current settings failed to persist: %v", err)
	}
	var persisted string
	if err := store.database.QueryRowContext(requestContext, "SELECT value FROM settings WHERE id=1").Scan(&persisted); err != nil {
		test.Fatal(err)
	}
	for _, removedField := range []string{"translatePaste", "translateUi", "selectionPaste", "selectionUi"} {
		if strings.Contains(persisted, removedField) {
			test.Fatalf("removed hotkey field %q remained persisted", removedField)
		}
	}
}
