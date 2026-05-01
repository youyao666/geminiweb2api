package gemini

import (
	"context"
	"encoding/json"
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

func TestExtractDeepThinkContent(t *testing.T) {
	rcNode := []interface{}{
		"rc_testid",
		[]interface{}{"placeholder text"},
		nil, nil, nil, nil, nil,
		[]interface{}{1},
		"zh",
		nil, nil,
		nil,
		nil, nil, nil, nil, nil, nil, nil, nil,
		[]interface{}{false},
		nil, nil, nil, nil, nil, nil,
		[]interface{}{},
		nil, nil, nil, nil, nil, nil, nil, nil,
		[]interface{}{
			[]interface{}{"**Step One**\n\nFirst thinking step.\n\n\n**Step Two**\n\nSecond thinking step.\n\n\n"},
			[]interface{}{
				[]interface{}{
					"**Step One**\n\nFirst thinking step.\n\n\n**Step Two**\n\nSecond thinking step.\n\n\n",
					"", "",
					[]interface{}{
						[]interface{}{nil, []interface{}{nil, 0, "Step One", nil}},
						[]interface{}{nil, []interface{}{nil, 0, "First thinking step."}},
						[]interface{}{nil, []interface{}{nil, 0, "Step Two", nil}},
						[]interface{}{nil, []interface{}{nil, 0, "Second thinking step."}},
					},
				},
			},
		},
	}

	inner := []interface{}{
		[]interface{}{rcNode},
		nil, nil,
		"rc_testid",
	}

	innerJSON, err := json.Marshal(inner)
	if err != nil {
		t.Fatal(err)
	}

	var result contentResult
	visitRCNodesV2(inner, &result)

	if result.Content == "" {
		t.Fatal("expected non-empty content")
	}
	if !strings.Contains(result.Content, "placeholder") {
		t.Fatalf("unexpected content: %s", result.Content)
	}
	if result.ReasoningContent == "" {
		t.Fatalf("expected non-empty reasoning content. rcNode len=%d, innerJSON=%s", len(rcNode), string(innerJSON))
	}
	if !strings.Contains(result.ReasoningContent, "Step One") {
		t.Fatalf("expected 'Step One' in reasoning, got: %s", result.ReasoningContent)
	}
	if !strings.Contains(result.ReasoningContent, "Step Two") {
		t.Fatalf("expected 'Step Two' in reasoning, got: %s", result.ReasoningContent)
	}
}

func TestExtractThinkingFromRCNode(t *testing.T) {
	payload := `["rc_test",["placeholder"],null,null,null,null,null,null,null,[1],"zh",null,null,null,null,null,null,null,null,null,null,null,[false],null,null,null,null,null,null,[],null,null,null,null,null,null,null,null,[["**Step 1**\n\nThinking text here\n\n\n**Step 2**\n\nMore thinking\n\n\n"]]]`
	var items []interface{}
	if err := json.Unmarshal([]byte(payload), &items); err != nil {
		t.Fatal(err)
	}
	thinking := extractThinkingFromRCNode(items)
	if thinking == "" {
		t.Fatal("expected non-empty thinking content")
	}
	if !strings.Contains(thinking, "Step 1") || !strings.Contains(thinking, "Step 2") {
		t.Fatalf("expected thinking to contain steps, got: %s", thinking)
	}
}

func TestExtractThinkingFromRCNode_NoThinking(t *testing.T) {
	payload := `["rc_test",["hello world"],null,null,null,null,null,null,null,[1],"zh"]`
	var items []interface{}
	if err := json.Unmarshal([]byte(payload), &items); err != nil {
		t.Fatal(err)
	}
	thinking := extractThinkingFromRCNode(items)
	if thinking != "" {
		t.Fatalf("expected empty thinking for normal response, got: %s", thinking)
	}
}

func TestExtractThinkingFromRCNode_IgnoresMetadata(t *testing.T) {
	items := []interface{}{
		"rc_4083678137dd176e",
		[]interface{}{"9.9更大。"},
		nil,
		[]interface{}{"rc_4083678137dd176e", "US", "e6fa609c3fa255c0", "e6fa609c3fa255c0", "3.1 Pro"},
	}

	thinking := extractThinkingFromRCNode(items)
	if thinking != "" {
		t.Fatalf("expected metadata to be ignored, got: %s", thinking)
	}
}
