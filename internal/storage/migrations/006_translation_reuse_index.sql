CREATE INDEX tasks_translation_reuse ON tasks (
  json_extract(value,'$.providerSnapshot.id'),
  json_extract(value,'$.sourceLanguage'),
  json_extract(value,'$.targetLanguage'),
  created_at DESC,
  id DESC
) WHERE status='succeeded';
