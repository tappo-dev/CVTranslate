INSERT OR IGNORE INTO providers(id,value) VALUES('builtin-microsoft','{"id":"builtin-microsoft","name":"Microsoft Translator","type":"microsoft-translate","baseUrl":"","model":"","timeoutSeconds":30,"enabled":true,"hasApiKey":false}');

UPDATE settings SET value=json_set(value,'$.defaultProviderId','builtin-microsoft')
WHERE json_extract(value,'$.defaultProviderId')='';
