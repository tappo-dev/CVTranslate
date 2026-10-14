package prompt

import (
	"strings"
	"testing"
)

func TestValidationAndOnePassRendering(t *testing.T) {
	for _, templates := range [][2]string{{"{{target_language}}", "{{text}}{{text}}"}, {"{{text}} {{target_language}}", "input"}, {"{{target_language}}", "{{unknown}} {{text}}"}, {"target", "{{text}}"}, {"{{target_language}}", "{{text}"}, {"{{target_language}}", "{{text}}}"}, {"{{target_language}}", strings.Repeat("x", MaxTemplateRunes) + "{{text}}"}} {
		if err := Validate(templates[0], templates[1]); err == nil {
			t.Errorf("accepted invalid template: %q", templates)
		}
	}
	template := BuiltIn()
	template.UserTemplate = " \n{{text}}\n "
	input := "hello {{target_language}}\n{{text}}"
	system, user, err := Render(template, input, "auto", "en")
	if err != nil {
		t.Fatal(err)
	}
	if user != " \n"+input+"\n " {
		t.Fatalf("input was rewritten: %q", user)
	}
	if !strings.Contains(system, "English") {
		t.Fatal("language not mapped")
	}
	if _, _, err := Render(template, input, "en", "auto"); err == nil {
		t.Fatal("automatic target accepted")
	}
}

func TestOptionalPersonaComposition(test *testing.T) {
	template := BuiltIn()
	template.Persona = ""
	sourceText := "Please fix {{text}} and keep {{target_language}} literal."
	baseSystem, baseUser, err := Render(template, sourceText, "en", "zh-CN")
	if err != nil {
		test.Fatal(err)
	}
	for _, emptyPersona := range []string{"", " \t\n"} {
		template.Persona = emptyPersona
		systemText, userText, err := Render(template, sourceText, "en", "zh-CN")
		if err != nil || systemText != baseSystem || userText != baseUser {
			test.Fatalf("disabled persona changed rendering: %v", err)
		}
	}
	template.Persona = "Optimistic and kind. Keep {{text}}, {{target_language}}, {braces} and 100% literal."
	systemText, userText, err := Render(template, sourceText, "en", "zh-CN")
	if err != nil {
		test.Fatal(err)
	}
	if !strings.HasPrefix(systemText, baseSystem+"\n\n") || strings.Count(systemText, template.Persona) != 1 || userText != sourceText {
		test.Fatal("persona composition changed the base prompt, persona or source text")
	}
	for _, instruction := range []string{"in Simplified Chinese", "softening profanity or blame", "Do not invent facts", "Meaning and the selected target language take priority", "Return only the final translation"} {
		if !strings.Contains(systemText, instruction) {
			test.Errorf("missing persona boundary: %s", instruction)
		}
	}
}

func TestBuiltInCodePreservationWithAndWithoutPersona(test *testing.T) {
	sourceText := "Please review this snippet:\r\n\r\n<thead>\r\n\t<tr><th scope=\"col\">\u65e5\u671f</th></tr>\r\n</thead>\r\n\r\n" +
		"```js\n// Keep {{target_language}} literal.\nconst label = \"\u65e5\u671f\";\n```\nUse `label` here.\n"
	for _, scenario := range []struct {
		name    string
		persona string
	}{
		{name: "without persona"},
		{name: "with default persona", persona: SeriousDeveloperPersona},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			template := BuiltIn()
			template.Persona = scenario.persona
			systemText, userText, err := Render(template, sourceText, "zh-CN", "en")
			if err != nil {
				test.Fatal(err)
			}
			if userText != sourceText {
				test.Fatal("rendering changed source code, whitespace or literal template syntax")
			}
			for _, instruction := range []string{
				"Translate only natural-language prose outside code",
				"fenced code blocks, indented code blocks, inline code, and unfenced source snippets",
				"comments, string literals, attribute values, and text between HTML/XML tags",
				"Preserve all whitespace, indentation, line breaks, punctuation, escapes, and existing code fences within code",
				"If the entire input is code, return it unchanged",
				"Do not add code fences around bare snippets",
				"Code preservation takes priority over target-language and persona/style instructions",
			} {
				if !strings.Contains(systemText, instruction) {
					test.Errorf("missing code preservation instruction: %s", instruction)
				}
			}
		})
	}
}

func TestPersonaLimitsApplyBeforeRendering(test *testing.T) {
	for _, scenario := range []struct {
		name    string
		persona string
		valid   bool
	}{
		{name: "unicode at limit", persona: strings.Repeat("\U0001f600", MaxPersonaRunes), valid: true},
		{name: "unicode above limit", persona: strings.Repeat("\U0001f600", MaxPersonaRunes+1)},
		{name: "invalid UTF-8", persona: string([]byte{0xff})},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			template := BuiltIn()
			template.Persona = scenario.persona
			_, _, err := Render(template, "source", "en", "zh-CN")
			if (err == nil) != scenario.valid {
				test.Fatalf("persona validation mismatch: %v", err)
			}
		})
	}
}
