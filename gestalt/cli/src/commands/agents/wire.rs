pub(crate) fn is_live_turn_status(status: &str) -> bool {
    matches!(status, "pending" | "running" | "")
}

pub(crate) fn is_terminal_turn_status(status: &str) -> bool {
    matches!(status, "succeeded" | "failed" | "canceled")
}

pub(crate) fn is_private_event_visibility(visibility: &str) -> bool {
    visibility == "private"
}

pub(crate) fn is_text_input_interaction(interaction_type: &str) -> bool {
    matches!(interaction_type, "clarification" | "input")
}
