use anyhow::{Context, Result};
use std::time::Duration;

use crate::api::ApiClient;

use super::requests::list_turn_event_values;
use super::types::AgentTurnEventInfo;

pub(crate) const DEFAULT_EVENT_PAGE_SIZE: u32 = 100;
pub(crate) const EVENT_POLL_INTERVAL: Duration = Duration::from_millis(250);

pub(crate) fn stream_turn_event_frames<F>(
    client: &ApiClient,
    turn_id: &str,
    after_seq: u64,
    mut handle_event: F,
) -> Result<()>
where
    F: FnMut(AgentTurnEventInfo) -> Result<()>,
{
    let raw_events = list_turn_event_values(client, turn_id, after_seq)?;
    for value in raw_events {
        let event: AgentTurnEventInfo =
            serde_json::from_value(value).context("failed to decode agent turn event")?;
        if event.seq > after_seq as i64 {
            handle_event(event)?;
        }
    }
    Ok(())
}
