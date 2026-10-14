package storage

import (
	"context"
	"testing"
	"time"

	"cvtranslate/internal/domain"
)

func TestTokenReportAggregatesOnlyRecordedAIUsage(test *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		test.Fatal(err)
	}
	defer store.Close()
	requestContext := context.Background()
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.UTC)
	usageToday := domain.TokenUsage{PromptTokens: 80, CompletionTokens: 20, TotalTokens: 100}
	usagePrevious := domain.TokenUsage{PromptTokens: 150, CompletionTokens: 50, TotalTokens: 200}
	for _, task := range []domain.Task{
		{ID: "usage-today", Status: "succeeded", Text: "source", Translation: "result", CreatedAt: today, TokenUsage: &usageToday},
		{ID: "usage-previous", Status: "succeeded", Text: "source", Translation: "result", CreatedAt: today.AddDate(0, 0, -6), TokenUsage: &usagePrevious},
		{ID: "no-usage", Status: "succeeded", Text: "source", Translation: "result", CreatedAt: today},
		{ID: "failed-usage", Status: "failed", Text: "source", CreatedAt: today, TokenUsage: &usageToday},
	} {
		if err := store.SaveTask(requestContext, task); err != nil {
			test.Fatal(err)
		}
	}
	if _, err := store.database.Exec(
		"UPDATE tasks SET value=json_set(value,'$.tokenUsage',json('{}')) WHERE id='no-usage'",
	); err != nil {
		test.Fatal(err)
	}
	report, err := store.GetTokenReport(requestContext, 365, 0)
	if err != nil {
		test.Fatal(err)
	}
	if len(report.Daily) != 365 {
		test.Fatalf("daily rows = %d", len(report.Daily))
	}
	if report.Today.RequestCount != 1 || report.Today.TotalTokens != 100 || report.Today.PromptTokens != 80 || report.Today.CompletionTokens != 20 {
		test.Fatalf("today summary = %+v", report.Today)
	}
	if report.Last7Days.RequestCount != 2 || report.Last7Days.TotalTokens != 300 {
		test.Fatalf("seven-day summary = %+v", report.Last7Days)
	}
	if report.Last30Days != report.Last7Days {
		test.Fatalf("monthly summary = %+v", report.Last30Days)
	}
}

func TestTokenReportUsesRequestedTimezone(test *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		test.Fatal(err)
	}
	defer store.Close()
	requestContext := context.Background()
	now := time.Now().UTC()
	usage := domain.TokenUsage{PromptTokens: 4, CompletionTokens: 2, TotalTokens: 6}
	task := domain.Task{
		ID: "timezone-boundary", Status: "succeeded", Text: "source", Translation: "result",
		CreatedAt: time.Date(now.Year(), now.Month(), now.Day()-1, 23, 30, 0, 0, time.UTC), TokenUsage: &usage,
	}
	if err := store.SaveTask(requestContext, task); err != nil {
		test.Fatal(err)
	}
	report, err := store.GetTokenReport(requestContext, 30, 8*60)
	if err != nil {
		test.Fatal(err)
	}
	expectedDate := task.CreatedAt.In(time.FixedZone("expected", 8*60*60)).Format("2006-01-02")
	found := false
	for _, day := range report.Daily {
		if day.Date == expectedDate && day.TotalTokens == usage.TotalTokens {
			found = true
		}
	}
	if !found {
		test.Fatalf("timezone date %s not found: %+v", expectedDate, report.Daily)
	}
}
