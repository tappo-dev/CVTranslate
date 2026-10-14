package storage

import (
	"context"
	"testing"
	"time"

	"cvtranslate/internal/domain"
)

func TestExistingProviderAndHistoryWithRemovedHTTPFlag(test *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		test.Fatal(err)
	}
	defer store.Close()
	configuration := domain.ProviderConfig{
		ID: "existing-provider", Name: "Local service", Type: "openai-compatible",
		BaseURL: "http://localhost:8080/v1", Model: "original-model",
		Enabled: true, TimeoutSeconds: 60,
	}
	requestContext := context.Background()
	if err := store.SaveProvider(requestContext, configuration); err != nil {
		test.Fatal(err)
	}
	task := domain.Task{
		ID: "existing-task", Status: "succeeded", Text: "original", Translation: "translated",
		ProviderSnapshot: configuration, CreatedAt: time.Now().UTC(),
	}
	if err := store.SaveTask(requestContext, task); err != nil {
		test.Fatal(err)
	}
	// Existing databases retain this JSON field until a record is saved again.
	if _, err := store.database.ExecContext(requestContext,
		"UPDATE providers SET value=json_set(value, '$.allowLocalHttp', json('true')) WHERE id=?", configuration.ID); err != nil {
		test.Fatal(err)
	}
	if _, err := store.database.ExecContext(requestContext,
		"UPDATE tasks SET value=json_set(value, '$.providerSnapshot.allowLocalHttp', json('true')) WHERE id=?", task.ID); err != nil {
		test.Fatal(err)
	}
	stored, err := store.GetProvider(requestContext, configuration.ID)
	if err != nil || stored != configuration {
		test.Fatalf("existing provider changed or became unreadable: %+v %v", stored, err)
	}
	history, err := store.ListTasks(requestContext, domain.TaskFilter{})
	if err != nil || len(history.Items) != 1 {
		test.Fatalf("existing history became unreadable: %+v %v", history, err)
	}
	previousTask := history.Items[0]
	if previousTask.ProviderSnapshot != configuration || previousTask.Text != task.Text || previousTask.Translation != task.Translation {
		test.Fatalf("existing task content or provider snapshot changed: %+v", previousTask)
	}
}

func TestTaskSnapshotsRemainImmutableAndSortChronologically(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	instant := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	task := domain.Task{ID: "first", Status: "queued", Text: "original", ProviderSnapshot: domain.ProviderConfig{Model: "original"}, CreatedAt: instant}
	if err := store.SaveTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	task.Status = "succeeded"
	task.Text = "changed"
	task.ProviderSnapshot.Model = "changed"
	task.Translation = "result"
	if err := store.SaveTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Text != "original" || stored.ProviderSnapshot.Model != "original" || stored.Translation != "result" {
		t.Fatalf("invalid snapshot: %+v", stored)
	}
	task.ID = "second"
	task.CreatedAt = instant.Add(time.Millisecond)
	if err := store.SaveTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListTasks(ctx, domain.TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].ID != "second" {
		t.Fatal("history not ordered chronologically")
	}
}
