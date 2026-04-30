package gemini

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestIsTransientNetworkError(t *testing.T) {
	if !isTransientNetworkError(context.DeadlineExceeded) {
		t.Fatal("expected context deadline exceeded to be transient")
	}
	if !isTransientNetworkError(errors.New("context deadline exceeded (Client.Timeout or context cancellation while reading body)")) {
		t.Fatal("expected client timeout while reading body to be transient")
	}
	if isTransientNetworkError(errors.New("Gemini returned login/consent page")) {
		t.Fatal("expected login/consent errors to remain non-transient")
	}
}

func TestParseToolCalls_FencedBlock(t *testing.T) {
	tools := []Tool{{Function: Function{Name: "get_weather"}}}
	content := "before\n```tool_call\n{\"name\":\"get_weather\",\"arguments\":{\"city\":\"Shanghai\",\"unit\":\"c\"}}\n```\nafter"

	clean, calls := parseToolCalls(content, tools)
	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "get_weather" {
		t.Fatalf("unexpected tool name: %s", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"city":"Shanghai","unit":"c"}` {
		t.Fatalf("unexpected arguments: %s", calls[0].Function.Arguments)
	}
	if strings.Contains(clean, "tool_call") {
		t.Fatalf("expected fenced block removed, got: %s", clean)
	}
}

func TestParseToolCalls_InlineAndStringifiedArgs(t *testing.T) {
	tools := []Tool{
		{Function: Function{Name: "search_web"}},
		{Function: Function{Name: "calculator"}},
	}
	content := strings.Join([]string{
		"Please run:",
		`{"name":"search_web","arguments":{"q":"golang regexp"}}`,
		"and then",
		`{"name":"calculator","arguments":"{\"expr\":\"15*37\"}"}`,
	}, "\n")

	clean, calls := parseToolCalls(content, tools)
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
	if calls[0].Function.Name != "search_web" || calls[0].Function.Arguments != `{"q":"golang regexp"}` {
		t.Fatalf("unexpected first call: %+v", calls[0])
	}
	if calls[1].Function.Name != "calculator" || calls[1].Function.Arguments != `{"expr":"15*37"}` {
		t.Fatalf("unexpected second call: %+v", calls[1])
	}
	if strings.Contains(clean, `"name":"search_web"`) || strings.Contains(clean, `"name":"calculator"`) {
		t.Fatalf("expected tool json removed from content, got: %s", clean)
	}
}

func TestParseToolCalls_IgnoreUnknownAndDeduplicate(t *testing.T) {
	tools := []Tool{{Function: Function{Name: "get_weather"}}}
	content := strings.Join([]string{
		`{"name":"unknown_tool","arguments":{"x":1}}`,
		`{"name":"get_weather","arguments":{"city":"Beijing"}}`,
		`{"name":"get_weather","arguments":{"city":"Beijing"}}`,
	}, "\n")

	clean, calls := parseToolCalls(content, tools)
	if len(calls) != 1 {
		t.Fatalf("expected 1 deduplicated tool call, got %d", len(calls))
	}
	if calls[0].Function.Arguments != `{"city":"Beijing"}` {
		t.Fatalf("unexpected arguments: %s", calls[0].Function.Arguments)
	}
	if !strings.Contains(clean, "unknown_tool") {
		t.Fatalf("unknown tool should be preserved in content, got: %s", clean)
	}
}
