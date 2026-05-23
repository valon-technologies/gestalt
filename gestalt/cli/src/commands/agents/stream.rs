use anyhow::{Context, Result};
use std::io::{BufRead, BufReader};
use std::time::Duration;

use crate::api::ApiClient;

use super::requests::turn_events_path;
use super::types::AgentTurnEventInfo;

pub(crate) const DEFAULT_EVENT_PAGE_SIZE: u32 = 100;
pub(crate) const EVENT_POLL_INTERVAL: Duration = Duration::from_millis(250);
pub(crate) const EVENT_STREAM_UNTIL_BLOCKED_OR_TERMINAL: &str = "blocked_or_terminal";

pub(crate) fn stream_turn_event_frames<F>(
    client: &ApiClient,
    turn_id: &str,
    after_seq: u64,
    mut handle_event: F,
) -> Result<()>
where
    F: FnMut(AgentTurnEventInfo) -> Result<()>,
{
    let resp = client
        .get_stream(&turn_events_path(
            turn_id,
            true,
            Some(after_seq),
            Some(DEFAULT_EVENT_PAGE_SIZE),
            Some(EVENT_STREAM_UNTIL_BLOCKED_OR_TERMINAL),
        ))
        .with_context(|| format!("failed to stream events for agent turn {turn_id}"))?;
    let mut reader = BufReader::new(resp);
    let mut line = String::new();
    let mut decoder = SseEventDecoder::default();

    loop {
        line.clear();
        let read = reader
            .read_line(&mut line)
            .context("failed to read agent turn event stream")?;
        if read == 0 {
            if let Some(event) = decoder.finish()? {
                handle_event(event)?;
            }
            return Ok(());
        }

        if let Some(event) = decoder.push_line(&line)? {
            handle_event(event)?;
        }
    }
}

#[derive(Default)]
pub(crate) struct SseEventDecoder {
    data: String,
}

impl SseEventDecoder {
    fn push_line(&mut self, line: &str) -> Result<Option<AgentTurnEventInfo>> {
        let trimmed = line.trim_end_matches(['\r', '\n']);
        if trimmed.is_empty() {
            return self.finish();
        }

        if let Some(value) = trimmed.strip_prefix("data:") {
            if !self.data.is_empty() {
                self.data.push('\n');
            }
            self.data.push_str(value.strip_prefix(' ').unwrap_or(value));
        }

        Ok(None)
    }

    fn finish(&mut self) -> Result<Option<AgentTurnEventInfo>> {
        if self.data.is_empty() {
            return Ok(None);
        }
        let raw = std::mem::take(&mut self.data);
        serde_json::from_str(&raw)
            .with_context(|| format!("failed to decode agent turn event stream frame: {raw}"))
            .map(Some)
    }
}
