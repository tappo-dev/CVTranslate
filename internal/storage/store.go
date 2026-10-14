// Package storage persists local history and configuration, never credentials.
package storage

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"cvtranslate/internal/domain"
	"cvtranslate/internal/prompt"
	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("record not found")
var ErrConflict = errors.New("operation conflicts with existing state")

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ database *sql.DB }

func Open(path string) (*Store, error) {
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	store := &Store{database: database}
	if err = store.initialize(context.Background()); err != nil {
		database.Close()
		return nil, err
	}
	return store, nil
}
func (store *Store) Close() error { return store.database.Close() }

func (store *Store) initialize(ctx context.Context) error {
	for _, statement := range []string{"PRAGMA busy_timeout=5000", "PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)"} {
		if _, err := store.database.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].Name() < entries[right].Name() })
	for _, entry := range entries {
		transaction, err := store.database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var exists int
		err = transaction.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations WHERE name=?", entry.Name()).Scan(&exists)
		if err == nil && exists == 0 {
			var script []byte
			script, err = migrations.ReadFile("migrations/" + entry.Name())
			if err == nil {
				_, err = transaction.ExecContext(ctx, string(script))
			}
			if err == nil {
				_, err = transaction.ExecContext(ctx, "INSERT INTO schema_migrations(name) VALUES(?)", entry.Name())
			}
		}
		if err != nil {
			transaction.Rollback()
			return err
		}
		if err = transaction.Commit(); err != nil {
			return err
		}
	}
	settings, _ := json.Marshal(domain.DefaultSettings())
	builtin, _ := json.Marshal(prompt.BuiltIn())
	if _, err := store.database.ExecContext(ctx, "INSERT OR IGNORE INTO settings(id,value) VALUES(1,?)", string(settings)); err != nil {
		return err
	}
	if _, err := store.database.ExecContext(ctx, "INSERT OR IGNORE INTO prompts(id,value) VALUES(?,?)", prompt.BuiltIn().ID, string(builtin)); err != nil {
		return err
	}
	rows, err := store.database.QueryContext(ctx, "SELECT value FROM tasks WHERE status IN ('queued','running')")
	if err != nil {
		return err
	}
	var unfinished []domain.Task
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			rows.Close()
			return err
		}
		var task domain.Task
		if err = json.Unmarshal([]byte(value), &task); err != nil {
			rows.Close()
			return err
		}
		unfinished = append(unfinished, task)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, task := range unfinished {
		task.Status = "interrupted"
		task.ErrorCode = "interrupted"
		task.ErrorMessage = "Application stopped before translation completed."
		task.UpdatedAt = time.Now().UTC()
		if err := store.SaveTask(ctx, task); err != nil {
			return err
		}
	}
	return nil
}

func getValue[Value any](ctx context.Context, database *sql.DB, table, identifier string) (Value, error) {
	var result Value
	var value string
	err := database.QueryRowContext(ctx, "SELECT value FROM "+table+" WHERE id=?", identifier).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(value), &result)
	return result, err
}
func listValues[Value any](ctx context.Context, database *sql.DB, table string) ([]Value, error) {
	rows, err := database.QueryContext(ctx, "SELECT value FROM "+table+" ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Value{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		var item Value
		if err := json.Unmarshal([]byte(value), &item); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func (store *Store) save(ctx context.Context, table, identifier string, value any) error {
	if identifier == "" || len(identifier) > 128 {
		return errors.New("invalid record identifier")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = store.database.ExecContext(ctx, "INSERT INTO "+table+"(id,value) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET value=excluded.value", identifier, string(encoded))
	return err
}
func (store *Store) ListProviders(ctx context.Context) ([]domain.ProviderConfig, error) {
	return listValues[domain.ProviderConfig](ctx, store.database, "providers")
}
func (store *Store) GetProvider(ctx context.Context, identifier string) (domain.ProviderConfig, error) {
	return getValue[domain.ProviderConfig](ctx, store.database, "providers", identifier)
}
func (store *Store) SaveProvider(ctx context.Context, value domain.ProviderConfig) error {
	value.HasAPIKey = false
	return store.save(ctx, "providers", value.ID, value)
}
func (store *Store) ListPrompts(ctx context.Context) ([]domain.Prompt, error) {
	return listValues[domain.Prompt](ctx, store.database, "prompts")
}
func (store *Store) GetPrompt(ctx context.Context, identifier string) (domain.Prompt, error) {
	return getValue[domain.Prompt](ctx, store.database, "prompts", identifier)
}
func (store *Store) SavePrompt(ctx context.Context, value domain.Prompt) error {
	if value.ID == prompt.BuiltIn().ID || value.BuiltIn {
		return ErrConflict
	}
	return store.save(ctx, "prompts", value.ID, value)
}
func (store *Store) GetSettings(ctx context.Context) (domain.Settings, error) {
	return getValue[domain.Settings](ctx, store.database, "settings", "1")
}
func (store *Store) SaveSettings(ctx context.Context, value domain.Settings) error {
	return store.save(ctx, "settings", "1", value)
}

func (store *Store) deleteConfig(ctx context.Context, table, identifier, defaultField string) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	var value string
	if err = transaction.QueryRowContext(ctx, "SELECT value FROM settings WHERE id=1").Scan(&value); err != nil {
		return err
	}
	var settings domain.Settings
	if err = json.Unmarshal([]byte(value), &settings); err != nil {
		return err
	}
	if defaultField == "provider" && settings.DefaultProviderID == identifier || defaultField == "prompt" && (settings.DefaultPromptID == identifier || identifier == prompt.BuiltIn().ID) {
		return ErrConflict
	}
	result, err := transaction.ExecContext(ctx, "DELETE FROM "+table+" WHERE id=?", identifier)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return transaction.Commit()
}
func (store *Store) DeleteProvider(ctx context.Context, identifier string) error {
	return store.deleteConfig(ctx, "providers", identifier, "provider")
}
func (store *Store) DeletePrompt(ctx context.Context, identifier string) error {
	return store.deleteConfig(ctx, "prompts", identifier, "prompt")
}

func (store *Store) SaveTask(ctx context.Context, task domain.Task) error {
	if task.ID == "" {
		return errors.New("task identifier is required")
	}
	if task.Status != "queued" && task.Status != "running" && !domain.IsTerminal(task.Status) {
		return errors.New("invalid task status")
	}
	task.ProviderSnapshot.HasAPIKey = false
	encoded, err := json.Marshal(task)
	if err != nil {
		return err
	}
	result, err := store.database.ExecContext(ctx, `INSERT INTO tasks(id,status,text,translation,created_at,value) VALUES(?,?,?,?,?,?)
	ON CONFLICT(id) DO UPDATE SET status=excluded.status,translation=excluded.translation,
	value=json_set(tasks.value,
	 '$.status',json_extract(excluded.value,'$.status'),
	 '$.translation',json_extract(excluded.value,'$.translation'),
	 '$.errorCode',json_extract(excluded.value,'$.errorCode'),
	 '$.errorMessage',json_extract(excluded.value,'$.errorMessage'),
	 '$.updatedAt',json_extract(excluded.value,'$.updatedAt'),
	 '$.durationMs',json_extract(excluded.value,'$.durationMs'),
	 '$.tokenUsage',json_extract(excluded.value,'$.tokenUsage'))
	WHERE tasks.status IN ('queued','running') AND NOT(tasks.status='running' AND excluded.status='queued')`, task.ID, task.Status, task.Text, task.Translation, task.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"), string(encoded))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrConflict
	}
	return nil
}
func (store *Store) GetTask(ctx context.Context, identifier string) (domain.Task, error) {
	return getValue[domain.Task](ctx, store.database, "tasks", identifier)
}

// FindReusableTask compares translation-affecting snapshots, not display names,
// timeouts, credentials or entry points. The source is matched byte-for-byte.
func (store *Store) FindReusableTask(ctx context.Context, candidate domain.Task) (*domain.Task, error) {
	var encoded string
	err := store.database.QueryRowContext(ctx, `SELECT value FROM tasks
	 WHERE status='succeeded' AND translation<>'' AND text=?
	   AND json_extract(value,'$.sourceLanguage')=?
	   AND json_extract(value,'$.targetLanguage')=?
	   AND json_extract(value,'$.providerSnapshot.id')=?
	   AND json_extract(value,'$.providerSnapshot.type')=?
	   AND json_extract(value,'$.providerSnapshot.baseUrl')=?
	   AND json_extract(value,'$.providerSnapshot.model')=?
	   AND json_extract(value,'$.promptSnapshot.id')=?
	   AND json_extract(value,'$.promptSnapshot.systemTemplate')=?
	   AND json_extract(value,'$.promptSnapshot.userTemplate')=?
	   AND coalesce(json_extract(value,'$.promptSnapshot.persona'),'')=?
	 ORDER BY created_at DESC,id DESC LIMIT 1`,
		candidate.Text, candidate.SourceLanguage, candidate.TargetLanguage,
		candidate.ProviderSnapshot.ID, candidate.ProviderSnapshot.Type,
		candidate.ProviderSnapshot.BaseURL, candidate.ProviderSnapshot.Model,
		candidate.PromptSnapshot.ID, candidate.PromptSnapshot.SystemTemplate,
		candidate.PromptSnapshot.UserTemplate, candidate.PromptSnapshot.Persona,
	).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var task domain.Task
	if err := json.Unmarshal([]byte(encoded), &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (store *Store) ListTasks(ctx context.Context, filter domain.TaskFilter) (domain.TaskPage, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Page > 1000000 {
		filter.Page = 1000000
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	page := domain.TaskPage{Items: []domain.Task{}, Page: filter.Page, PageSize: filter.PageSize}
	if len(filter.Query) > 2000 {
		return page, errors.New("search query exceeds limit")
	}
	if !filter.CreatedAfter.IsZero() && !filter.CreatedBefore.IsZero() && !filter.CreatedAfter.Before(filter.CreatedBefore) {
		return page, errors.New("invalid task date range")
	}
	where := " WHERE 1=1"
	arguments := []any{}
	if filter.Status != "" {
		if filter.Status != "queued" && filter.Status != "running" && !domain.IsTerminal(filter.Status) {
			return page, errors.New("invalid task status")
		}
		where += " AND status=?"
		arguments = append(arguments, filter.Status)
	}
	if filter.Query != "" {
		escaped := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(filter.Query)
		where += ` AND (text LIKE ? ESCAPE '\' OR translation LIKE ? ESCAPE '\')`
		arguments = append(arguments, "%"+escaped+"%", "%"+escaped+"%")
	}
	if !filter.CreatedAfter.IsZero() {
		where += " AND created_at>=?"
		arguments = append(arguments, filter.CreatedAfter.UTC().Format("2006-01-02T15:04:05.000000000Z"))
	}
	if !filter.CreatedBefore.IsZero() {
		where += " AND created_at<?"
		arguments = append(arguments, filter.CreatedBefore.UTC().Format("2006-01-02T15:04:05.000000000Z"))
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer transaction.Rollback()
	if err := transaction.QueryRowContext(ctx, "SELECT count(*) FROM tasks"+where, arguments...).Scan(&page.Total); err != nil {
		return page, err
	}
	arguments = append(arguments, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := transaction.QueryContext(ctx, "SELECT value FROM tasks"+where+" ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", arguments...)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			rows.Close()
			return page, err
		}
		var task domain.Task
		if err = json.Unmarshal([]byte(value), &task); err != nil {
			rows.Close()
			return page, err
		}
		page.Items = append(page.Items, task)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	return page, transaction.Commit()
}

func addTokenUsage(summary *domain.TokenUsageSummary, usage domain.TokenUsage) {
	summary.RequestCount++
	summary.PromptTokens += usage.PromptTokens
	summary.CompletionTokens += usage.CompletionTokens
	summary.TotalTokens += usage.TotalTokens
}

func (store *Store) GetTokenReport(ctx context.Context, days, timezoneOffsetMinutes int) (domain.TokenReport, error) {
	if days < 30 || days > 366 || timezoneOffsetMinutes < -14*60 || timezoneOffsetMinutes > 14*60 {
		return domain.TokenReport{}, errors.New("invalid token report range")
	}
	location := time.FixedZone("report", timezoneOffsetMinutes*60)
	now := time.Now().In(location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
	firstDay := today.AddDate(0, 0, -(days - 1))
	tomorrow := today.AddDate(0, 0, 1)
	rows, err := store.database.QueryContext(ctx, `SELECT created_at,
	 json_extract(value,'$.tokenUsage.promptTokens'),
	 json_extract(value,'$.tokenUsage.completionTokens'),
	 json_extract(value,'$.tokenUsage.totalTokens')
	 FROM tasks
	 WHERE status='succeeded'
	   AND json_type(value,'$.tokenUsage')='object'
	   AND json_type(value,'$.tokenUsage.promptTokens')='integer'
	   AND json_type(value,'$.tokenUsage.completionTokens')='integer'
	   AND json_type(value,'$.tokenUsage.totalTokens')='integer'
	   AND json_extract(value,'$.tokenUsage.promptTokens')>=0
	   AND json_extract(value,'$.tokenUsage.completionTokens')>=0
	   AND json_extract(value,'$.tokenUsage.totalTokens')>=
	       json_extract(value,'$.tokenUsage.promptTokens')+
	       json_extract(value,'$.tokenUsage.completionTokens')
	   AND created_at>=?
	   AND created_at<?
	 ORDER BY created_at`,
		firstDay.UTC().Format("2006-01-02T15:04:05.000000000Z"),
		tomorrow.UTC().Format("2006-01-02T15:04:05.000000000Z"),
	)
	if err != nil {
		return domain.TokenReport{}, err
	}
	defer rows.Close()
	usageByDate := make(map[string]domain.TokenUsageSummary, days)
	for rows.Next() {
		var createdAt string
		var usage domain.TokenUsage
		if err := rows.Scan(&createdAt, &usage.PromptTokens, &usage.CompletionTokens, &usage.TotalTokens); err != nil {
			return domain.TokenReport{}, err
		}
		instant, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return domain.TokenReport{}, err
		}
		date := instant.In(location).Format("2006-01-02")
		summary := usageByDate[date]
		addTokenUsage(&summary, usage)
		usageByDate[date] = summary
	}
	if err := rows.Err(); err != nil {
		return domain.TokenReport{}, err
	}
	report := domain.TokenReport{Daily: make([]domain.DailyTokenUsage, 0, days)}
	for dayIndex := 0; dayIndex < days; dayIndex++ {
		date := firstDay.AddDate(0, 0, dayIndex).Format("2006-01-02")
		summary := usageByDate[date]
		report.Daily = append(report.Daily, domain.DailyTokenUsage{Date: date, TokenUsageSummary: summary})
		daysAgo := days - dayIndex - 1
		if daysAgo == 0 {
			report.Today = summary
		}
		if daysAgo < 7 {
			report.Last7Days.RequestCount += summary.RequestCount
			report.Last7Days.PromptTokens += summary.PromptTokens
			report.Last7Days.CompletionTokens += summary.CompletionTokens
			report.Last7Days.TotalTokens += summary.TotalTokens
		}
		if daysAgo < 30 {
			report.Last30Days.RequestCount += summary.RequestCount
			report.Last30Days.PromptTokens += summary.PromptTokens
			report.Last30Days.CompletionTokens += summary.CompletionTokens
			report.Last30Days.TotalTokens += summary.TotalTokens
		}
	}
	return report, nil
}

func (store *Store) DeleteTask(ctx context.Context, identifier string) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	var status string
	if err = transaction.QueryRowContext(ctx, "SELECT status FROM tasks WHERE id=?", identifier).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if !domain.IsTerminal(status) {
		return ErrConflict
	}
	if _, err = transaction.ExecContext(ctx, "DELETE FROM tasks WHERE id=?", identifier); err != nil {
		return err
	}
	return transaction.Commit()
}
func (store *Store) ClearTasks(ctx context.Context) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	var count int
	if err = transaction.QueryRowContext(ctx, "SELECT count(*) FROM tasks WHERE status IN ('queued','running')").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return ErrConflict
	}
	if _, err = transaction.ExecContext(ctx, "DELETE FROM tasks"); err != nil {
		return err
	}
	return transaction.Commit()
}
func (store *Store) UpdatePasteStatus(ctx context.Context, identifier, status string) error {
	if len(status) > 100 {
		return errors.New("paste status exceeds limit")
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	var value string
	if err = transaction.QueryRowContext(ctx, "SELECT value FROM tasks WHERE id=?", identifier).Scan(&value); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	var task domain.Task
	if err = json.Unmarshal([]byte(value), &task); err != nil {
		return err
	}
	if !domain.IsTerminal(task.Status) {
		return ErrConflict
	}
	task.PasteStatus = status
	task.UpdatedAt = time.Now().UTC()
	encoded, err := json.Marshal(task)
	if err != nil {
		return err
	}
	if _, err = transaction.ExecContext(ctx, "UPDATE tasks SET value=? WHERE id=?", string(encoded), identifier); err != nil {
		return err
	}
	return transaction.Commit()
}
