use std::sync::mpsc::{Receiver, Sender, TryRecvError};
use std::thread;
use std::time::Duration;

use anyhow::{Context, Result, anyhow};
use serde_json::{Map, Value};

use crate::api::ApiClient;
use crate::cli::AgentTurnCreateArgs;

use super::super::{
    AgentInteractionInfo, AgentTurnEventInfo, AgentTurnInfo, INTERRUPT_CANCEL_REASON,
    cancel_turn_silent, create_turn_info, get_turn_info, list_interactions_info,
    resolve_interaction_info, stream_turn_event_frames,
};

pub(super) struct TurnWorker {
    pub(super) command_tx: Sender<WorkerCommand>,
    pub(super) handle: Option<thread::JoinHandle<()>>,
}

pub(super) enum WorkerCommand {
    Resolve {
        interaction_id: String,
        resolution: Map<String, Value>,
    },
    Cancel,
}

pub(super) enum WorkerEvent {
    TurnCreated(AgentTurnInfo),
    TurnEvent(Box<AgentTurnEventInfo>),
    TurnSnapshot(AgentTurnInfo),
    WaitingForInput(AgentInteractionInfo),
    InteractionResolved(AgentInteractionInfo),
    Error(String),
    Done,
}

pub(super) fn spawn_turn_worker(
    client: ApiClient,
    turn_args: AgentTurnCreateArgs,
    event_tx: Sender<WorkerEvent>,
    command_rx: Receiver<WorkerCommand>,
) -> thread::JoinHandle<()> {
    thread::spawn(move || {
        let result = run_turn_worker(&client, turn_args, &event_tx, &command_rx);
        if let Err(err) = result {
            let _ = event_tx.send(WorkerEvent::Error(format!("{err:#}")));
        }
        let _ = event_tx.send(WorkerEvent::Done);
    })
}

fn run_turn_worker(
    client: &ApiClient,
    turn_args: AgentTurnCreateArgs,
    event_tx: &Sender<WorkerEvent>,
    command_rx: &Receiver<WorkerCommand>,
) -> Result<()> {
    let turn = create_turn_info(client, &turn_args)?;
    let turn_id = turn.id.clone();
    event_tx
        .send(WorkerEvent::TurnCreated(turn))
        .context("terminal UI closed before turn started")?;
    if consume_cancel(client, &turn_id, command_rx) {
        return Ok(());
    }

    let mut after_seq = 0;
    loop {
        stream_turn_event_frames(client, &turn_id, after_seq, |event| {
            if event.seq > 0 {
                after_seq = after_seq.max(event.seq as u64);
            }
            event_tx
                .send(WorkerEvent::TurnEvent(Box::new(event)))
                .context("terminal UI closed while streaming events")
        })?;

        if consume_cancel(client, &turn_id, command_rx) {
            return Ok(());
        }

        let latest = get_turn_info(client, &turn_id)?;
        event_tx
            .send(WorkerEvent::TurnSnapshot(latest.clone()))
            .context("terminal UI closed before turn snapshot")?;

        match latest.status.as_str() {
            "waiting_for_input" => {
                let interactions = list_interactions_info(client, &turn_id)?;
                let pending: Vec<_> = interactions
                    .into_iter()
                    .filter(|interaction| interaction.state == "pending")
                    .collect();
                if pending.is_empty() {
                    return Err(anyhow!(
                        "agent turn {} is waiting for input without a pending interaction",
                        latest.id
                    ));
                }
                for interaction in pending {
                    event_tx
                        .send(WorkerEvent::WaitingForInput(interaction.clone()))
                        .context("terminal UI closed before interaction prompt")?;
                    wait_for_interaction_resolution(
                        client,
                        &turn_id,
                        interaction,
                        command_rx,
                        event_tx,
                    )?;
                }
            }
            "pending" | "running" => thread::sleep(Duration::from_millis(250)),
            "succeeded" | "failed" | "canceled" => return Ok(()),
            other => {
                return Err(anyhow!(
                    "agent turn {} has unsupported status {}",
                    latest.id,
                    other
                ));
            }
        }
    }
}

fn consume_cancel(client: &ApiClient, turn_id: &str, command_rx: &Receiver<WorkerCommand>) -> bool {
    match command_rx.try_recv() {
        Ok(WorkerCommand::Cancel) => {
            let _ = cancel_turn_silent(client, turn_id, INTERRUPT_CANCEL_REASON);
            true
        }
        Ok(WorkerCommand::Resolve { .. }) => false,
        Err(TryRecvError::Empty | TryRecvError::Disconnected) => false,
    }
}

fn wait_for_interaction_resolution(
    client: &ApiClient,
    turn_id: &str,
    interaction: AgentInteractionInfo,
    command_rx: &Receiver<WorkerCommand>,
    event_tx: &Sender<WorkerEvent>,
) -> Result<()> {
    loop {
        match command_rx
            .recv()
            .context("terminal UI closed before resolving interaction")?
        {
            WorkerCommand::Cancel => {
                let _ = cancel_turn_silent(client, turn_id, INTERRUPT_CANCEL_REASON);
                return Ok(());
            }
            WorkerCommand::Resolve {
                interaction_id,
                resolution,
            } if interaction_id == interaction.id => {
                let resolved =
                    resolve_interaction_info(client, turn_id, &interaction.id, resolution)?;
                event_tx
                    .send(WorkerEvent::InteractionResolved(resolved))
                    .context("terminal UI closed after resolving interaction")?;
                return Ok(());
            }
            WorkerCommand::Resolve { .. } => {}
        }
    }
}
