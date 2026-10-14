UPDATE settings
SET value=json_set(value,'$.speech',json('{"engine":"edge","edgeVoices":{"en":"en-US-AndrewMultilingualNeural","zh-CN":"zh-CN-XiaoxiaoMultilingualNeural"},"webSpeechVoices":{"en":"","zh-CN":""}}'))
WHERE json_type(value,'$.speech') IS NULL;
