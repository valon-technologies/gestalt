use anyhow::{Result, bail};
use std::thread;

use crate::api::ApiClient;

use super::requests::{get_turn_info, list_interactions_info};
use super::stream::EVENT_POLL_INTERVAL;
use super::types::{AgentInteractionInfo, AgentTurnInfo};
use super::wire::{is_live_turn_status, is_terminal_turn_status};

pub(crate) struct TurnLoopContext<'a> {
    pub client: &'a ApiClient,
    pub turn_id: &'a str,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub(crate) enum TurnLoopOutcome {
    Continue,
    Cancelled,
}

pub(crate) trait TurnDriverSink {
    fn stream_events(&mut self, ctx: &TurnLoopContext<'_>) -> Result<()>;
    fn on_turn_snapshot(&mut self, turn: &AgentTurnInfo) -> Result<()>;
    fn handle_waiting_for_input(
        &mut self,
        ctx: &TurnLoopContext<'_>,
        turn: &AgentTurnInfo,
    ) -> Result<TurnLoopOutcome>;
    fn poll_cancel(&mut self, ctx: &TurnLoopContext<'_>) -> Result<bool>;
}

pub(crate) fn run_turn_status_loop<S: TurnDriverSink>(
    ctx: &TurnLoopContext<'_>,
    sink: &mut S,
) -> Result<()> {
    loop {
        sink.stream_events(ctx)?;
        if sink.poll_cancel(ctx)? {
            return Ok(());
        }
        let latest = get_turn_info(ctx.client, ctx.turn_id)?;
        sink.on_turn_snapshot(&latest)?;
        let status = latest.status.as_str();
        if status == "waiting_for_input" {
            match sink.handle_waiting_for_input(ctx, &latest)? {
                TurnLoopOutcome::Continue => {}
                TurnLoopOutcome::Cancelled => return Ok(()),
            }
        } else if is_live_turn_status(status) {
            thread::sleep(EVENT_POLL_INTERVAL);
        } else if is_terminal_turn_status(status) {
            return Ok(());
        } else {
            bail!("agent turn {} has unsupported status {}", latest.id, status);
        }
    }
}

pub(crate) fn resolve_pending_interactions(
    ctx: &TurnLoopContext<'_>,
    turn: &AgentTurnInfo,
    mut resolve_one: impl FnMut(AgentInteractionInfo) -> Result<TurnLoopOutcome>,
) -> Result<TurnLoopOutcome> {
    let interactions = list_interactions_info(ctx.client, &turn.id)?;
    let pending: Vec<_> = interactions
        .into_iter()
        .filter(|interaction| interaction.state == "pending")
        .collect();
    if pending.is_empty() {
        bail!(
            "agent turn {} is waiting for input without a pending interaction",
            turn.id
        );
    }
    for interaction in pending {
        match resolve_one(interaction)? {
            TurnLoopOutcome::Continue => {}
            outcome => return Ok(outcome),
        }
    }
    Ok(TurnLoopOutcome::Continue)
}
