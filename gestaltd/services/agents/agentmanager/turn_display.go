package agentmanager

import (
	"fmt"
	"maps"
	"strings"

	coreagent "github.com/valon-technologies/gestalt/server/core/agent"
)

func normalizeTurnEventsForDisplay(events []*coreagent.TurnEvent) []*coreagent.TurnEvent {
	if len(events) == 0 {
		return nil
	}
	out := make([]*coreagent.TurnEvent, 0, len(events))
	for _, event := range events {
		if event == nil {
			continue
		}
		next := cloneTurnEventForDisplay(event)
		if next.Display == nil {
			next.Display = synthesizeTurnDisplay(next)
		} else {
			normalizeTurnDisplay(next)
		}
		out = append(out, next)
	}
	return out
}

func cloneTurnEventForDisplay(event *coreagent.TurnEvent) *coreagent.TurnEvent {
	next := *event
	next.Data = maps.Clone(event.Data)
	if event.Display != nil {
		display := *event.Display
		next.Display = &display
	}
	return &next
}

func synthesizeTurnDisplay(event *coreagent.TurnEvent) *coreagent.TurnDisplay {
	if event == nil {
		return nil
	}
	switch strings.TrimSpace(event.Type) {
	case "agent.message.delta", "assistant.delta":
		text := displayStringField(event.Data, "text", "delta", "content")
		if text == "" {
			return nil
		}
		return &coreagent.TurnDisplay{Kind: "text", Phase: "delta", Text: text, Format: "markdown"}
	case "assistant.completed":
		return &coreagent.TurnDisplay{
			Kind:   "text",
			Phase:  "completed",
			Text:   displayStringField(event.Data, "text"),
			Format: "markdown",
		}
	case "tool.started":
		return &coreagent.TurnDisplay{
			Kind:      "tool",
			Phase:     "started",
			Action:    "Running",
			Label:     displayToolLabel(event.Data),
			Ref:       displayToolRef(event.Data),
			ParentRef: displayStringField(event.Data, "parent_ref", "parentRef", "parent_call_id", "parentCallId"),
			Input:     displayValueField(event.Data, "display_input", "displayInput", "input_preview", "inputPreview", "arguments_preview", "argumentsPreview", "arguments", "input", "request"),
		}
	case "tool.completed":
		return &coreagent.TurnDisplay{
			Kind:      "tool",
			Phase:     "completed",
			Action:    "Ran",
			Text:      displayStatusText(event.Data),
			Label:     displayToolLabel(event.Data),
			Ref:       displayToolRef(event.Data),
			ParentRef: displayStringField(event.Data, "parent_ref", "parentRef", "parent_call_id", "parentCallId"),
			Output:    displayValueField(event.Data, "display_output", "displayOutput", "output_preview", "outputPreview", "result_preview", "resultPreview", "output", "result", "body"),
			Error:     displayValueField(event.Data, "display_error", "displayError", "error"),
		}
	case "tool.failed":
		err := displayValueField(event.Data, "display_error", "displayError", "error")
		return &coreagent.TurnDisplay{
			Kind:      "tool",
			Phase:     "failed",
			Action:    "Failed",
			Text:      displayErrorText(err),
			Label:     displayToolLabel(event.Data),
			Ref:       displayToolRef(event.Data),
			ParentRef: displayStringField(event.Data, "parent_ref", "parentRef", "parent_call_id", "parentCallId"),
			Error:     err,
		}
	case "interaction.requested":
		ref := displayStringField(event.Data, "interaction_id", "interactionId")
		return &coreagent.TurnDisplay{Kind: "interaction", Phase: "requested", Ref: ref, Label: "interaction"}
	case "interaction.resolved":
		ref := displayStringField(event.Data, "interaction_id", "interactionId")
		return &coreagent.TurnDisplay{Kind: "interaction", Phase: "resolved", Ref: ref, Label: "interaction"}
	case "turn.started":
		return &coreagent.TurnDisplay{Kind: "status", Phase: "started", Label: "turn", Text: displayStatusText(event.Data)}
	case "turn.failed":
		text := displayStringField(event.Data, "error", "message", "status")
		return &coreagent.TurnDisplay{Kind: "error", Phase: "failed", Label: "turn", Text: text}
	case "turn.canceled":
		text := displayStringField(event.Data, "reason", "message", "status")
		return &coreagent.TurnDisplay{Kind: "status", Phase: "canceled", Label: "turn", Text: text}
	default:
		return nil
	}
}

func normalizeTurnDisplay(event *coreagent.TurnEvent) {
	if event == nil || event.Display == nil {
		return
	}
	display := event.Display
	display.Kind = strings.TrimSpace(display.Kind)
	display.Phase = strings.TrimSpace(display.Phase)
	display.Action = strings.TrimSpace(display.Action)
	display.Format = strings.TrimSpace(display.Format)
	display.Language = strings.TrimSpace(display.Language)
	if display.Action == "" && display.Kind == "tool" {
		display.Action = displayToolAction(display.Phase)
	}
	if display.Format == "" && display.Kind == "text" && knownAssistantTextEvent(event.Type) {
		display.Format = "markdown"
	}
}

func displayToolAction(phase string) string {
	switch strings.TrimSpace(phase) {
	case "started", "progress":
		return "Running"
	case "completed":
		return "Ran"
	case "failed":
		return "Failed"
	case "canceled":
		return "Canceled"
	default:
		return ""
	}
}

func knownAssistantTextEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "agent.message.delta", "assistant.delta", "assistant.completed":
		return true
	default:
		return false
	}
}

func displayToolLabel(data map[string]any) string {
	label := displayStringField(data, "tool_name", "toolName", "name", "operation", "tool_id", "toolId")
	if strings.TrimSpace(label) == "" {
		return "tool"
	}
	return label
}

func displayToolRef(data map[string]any) string {
	return displayStringField(data, "tool_call_id", "toolCallId", "call_id", "callId", "id")
}

func displayStatusText(data map[string]any) string {
	if text := displayStringField(data, "text", "message"); text != "" {
		return text
	}
	if status := displayStringField(data, "status", "state"); status != "" {
		return status
	}
	if status := displayValueField(data, "statusCode", "status_code"); status != nil {
		return fmt.Sprint(status)
	}
	return ""
}

func displayErrorText(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func displayStringField(data map[string]any, keys ...string) string {
	if len(data) == 0 {
		return ""
	}
	for _, key := range keys {
		value, ok := data[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			return strings.TrimSpace(typed)
		default:
			return strings.TrimSpace(fmt.Sprint(typed))
		}
	}
	return ""
}

func displayValueField(data map[string]any, keys ...string) any {
	if len(data) == 0 {
		return nil
	}
	for _, key := range keys {
		if value, ok := data[key]; ok && value != nil {
			return value
		}
	}
	return nil
}
