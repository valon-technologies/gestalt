use std::sync::mpsc::{Receiver, Sender, TryRecvError};
use std::thread;

use anyhow::{Context, Result};
use serde_json::{Map, Value};

use crate::api::ApiClient;
use crate::cli::AgentTurnCreateArgs;
use crate::commands::agents::driver::{
    TurnDriverSink, TurnLoopContext, TurnLoopOutcome, resolve_pending_interactions,
    run_turn_status_loop,
};
use crate::commands::agents::requests::{
    INTERRUPT_CANCEL_REASON, cancel_turn_silent, create_turn_info, resolve_interaction_info,
};
use crate::commands::agents::stream::stream_turn_event_frames;
use crate::commands::agents::types::{AgentInteractionInfo, AgentTurnEventInfo, AgentTurnInfo};

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

    let ctx = TurnLoopContext {
        client,
        turn_id: &turn_id,
    };
    let mut sink = WorkerTurnDriver {
        after_seq: 0,
        event_tx,
        command_rx,
    };
    run_turn_status_loop(&ctx, &mut sink)
}

struct WorkerTurnDriver<'a> {
    after_seq: u64,
    event_tx: &'a Sender<WorkerEvent>,
    command_rx: &'a Receiver<WorkerCommand>,
}

impl TurnDriverSink for WorkerTurnDriver<'_> {
    fn stream_events(&mut self, ctx: &TurnLoopContext<'_>) -> Result<()> {
        stream_turn_event_frames(ctx.client, ctx.turn_id, self.after_seq, |event| {
            if event.seq > 0 {
                self.after_seq = self.after_seq.max(event.seq as u64);
            }
            self.event_tx
                .send(WorkerEvent::TurnEvent(Box::new(event)))
                .context("terminal UI closed while streaming events")
        })
    }

    fn on_turn_snapshot(&mut self, turn: &AgentTurnInfo) -> Result<()> {
        self.event_tx
            .send(WorkerEvent::TurnSnapshot(turn.clone()))
            .context("terminal UI closed before turn snapshot")
    }

    fn handle_waiting_for_input(
        &mut self,
        ctx: &TurnLoopContext<'_>,
        turn: &AgentTurnInfo,
    ) -> Result<TurnLoopOutcome> {
        resolve_pending_interactions(ctx, turn, |interaction| {
            self.event_tx
                .send(WorkerEvent::WaitingForInput(interaction.clone()))
                .context("terminal UI closed before interaction prompt")?;
            wait_for_interaction_resolution(ctx, &interaction, self.command_rx, self.event_tx)
        })
    }

    fn poll_cancel(&mut self, ctx: &TurnLoopContext<'_>) -> Result<bool> {
        Ok(consume_cancel(ctx.client, ctx.turn_id, self.command_rx))
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
    ctx: &TurnLoopContext<'_>,
    interaction: &AgentInteractionInfo,
    command_rx: &Receiver<WorkerCommand>,
    event_tx: &Sender<WorkerEvent>,
) -> Result<TurnLoopOutcome> {
    loop {
        match command_rx
            .recv()
            .context("terminal UI closed before resolving interaction")?
        {
            WorkerCommand::Cancel => {
                let _ = cancel_turn_silent(ctx.client, ctx.turn_id, INTERRUPT_CANCEL_REASON);
                return Ok(TurnLoopOutcome::Cancelled);
            }
            WorkerCommand::Resolve {
                interaction_id,
                resolution,
            } if interaction_id == interaction.id => {
                let resolved = resolve_interaction_info(
                    ctx.client,
                    ctx.turn_id,
                    &interaction.id,
                    resolution,
                )?;
                event_tx
                    .send(WorkerEvent::InteractionResolved(resolved))
                    .context("terminal UI closed after resolving interaction")?;
                return Ok(TurnLoopOutcome::Continue);
            }
            WorkerCommand::Resolve { .. } => {}
        }
    }
}
