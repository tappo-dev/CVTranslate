UPDATE prompts
SET value = json_insert(value, '$.persona', '')
WHERE json_type(value, '$.persona') IS NULL;
