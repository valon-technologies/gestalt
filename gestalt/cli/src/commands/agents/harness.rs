use anyhow::{Context, Result, bail};
use serde_json::{Map, Value};
use std::io::{self, IsTerminal};
use std::path::{Path, PathBuf};
use std::process::{Command as ProcessCommand, ExitStatus, Stdio};

use crate::api::ApiClient;
use crate::interactive::prompt_confirm;

const HARNESSES_RESOLVE_PATH: &str = "/api/v1/agent/harnesses/resolve";
use super::types::{AgentHarnessInstallCommand, AgentHarnessPlan, AgentHarnessResolveRequest};

pub fn launch_local(
    client: &ApiClient,
    provider: Option<&str>,
    harness: Option<&str>,
) -> Result<()> {
    let plan = resolve_agent_harness(client, provider, harness)?;
    let prepared = prepare_agent_harness(&plan, HarnessInstallPolicy::Prompt)?;
    launch_agent_harness(&plan, &prepared)
}

pub fn doctor_local(
    client: &ApiClient,
    provider: Option<&str>,
    harness: Option<&str>,
) -> Result<()> {
    let plan = resolve_agent_harness(client, provider, harness)?;
    let _ = prepare_agent_harness(&plan, HarnessInstallPolicy::CheckOnly)?;
    let harness_label = if plan.harness.trim().is_empty() {
        plan.provider.clone()
    } else {
        format!("{}/{}", plan.provider, plan.harness)
    };
    println!("agent harness {:?} ok", harness_label);
    Ok(())
}

fn resolve_agent_harness(
    client: &ApiClient,
    provider: Option<&str>,
    harness: Option<&str>,
) -> Result<AgentHarnessPlan> {
    let body = AgentHarnessResolveRequest { provider, harness };
    let resp = client
        .post(HARNESSES_RESOLVE_PATH, &body)
        .context("failed to resolve agent harness")?;
    serde_json::from_value(resp).context("invalid agent harness response")
}

struct PreparedAgentHarness {
    command: PathBuf,
    env: Vec<String>,
    working_directory: Option<PathBuf>,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum HarnessInstallPolicy {
    CheckOnly,
    Prompt,
}

struct MissingHarnessCommand {
    command: String,
    role: MissingHarnessCommandRole,
    detail: String,
}

#[derive(Clone, Copy)]
enum MissingHarnessCommandRole {
    Required,
    HarnessCommand,
}

fn prepare_agent_harness(
    plan: &AgentHarnessPlan,
    install_policy: HarnessInstallPolicy,
) -> Result<PreparedAgentHarness> {
    let mut attempted_install = false;
    loop {
        let prepared = prepare_agent_harness_once(plan, install_policy, attempted_install)?;
        match prepared {
            PrepareAgentHarnessResult::Ready(prepared) => return Ok(prepared),
            PrepareAgentHarnessResult::Installed => {
                attempted_install = true;
            }
        }
    }
}

enum PrepareAgentHarnessResult {
    Ready(PreparedAgentHarness),
    Installed,
}

fn prepare_agent_harness_once(
    plan: &AgentHarnessPlan,
    install_policy: HarnessInstallPolicy,
    attempted_install: bool,
) -> Result<PrepareAgentHarnessResult> {
    let env = effective_harness_env(&plan.env)?;
    let working_directory = if plan.working_directory.trim().is_empty() {
        None
    } else {
        let path = PathBuf::from(plan.working_directory.trim());
        let metadata = std::fs::metadata(&path).with_context(|| {
            format!(
                "agent harness working directory {:?} is not accessible",
                path.display().to_string()
            )
        })?;
        if !metadata.is_dir() {
            bail!(
                "agent harness working directory {:?} is not a directory",
                path.display().to_string()
            );
        }
        Some(path)
    };

    let missing = missing_harness_commands(plan, working_directory.as_deref(), &env);
    if !missing.is_empty() {
        if install_policy == HarnessInstallPolicy::Prompt
            && !attempted_install
            && maybe_run_agent_harness_install(plan, &missing, working_directory.as_deref(), &env)?
        {
            return Ok(PrepareAgentHarnessResult::Installed);
        }
        bail!("{}", missing_harness_commands_message(plan, &missing));
    }
    let command = find_command(plan.command.trim(), working_directory.as_deref(), &env)
        .with_context(|| format!("agent harness command {:?} is not available", plan.command))?;
    Ok(PrepareAgentHarnessResult::Ready(PreparedAgentHarness {
        command,
        env: env_list(env),
        working_directory,
    }))
}

fn missing_harness_commands(
    plan: &AgentHarnessPlan,
    working_directory: Option<&Path>,
    env: &std::collections::BTreeMap<String, String>,
) -> Vec<MissingHarnessCommand> {
    let mut missing = Vec::new();
    for required in &plan.required_commands {
        let required = required.trim();
        if required.is_empty() {
            continue;
        }
        if let Err(err) = find_command(required, working_directory, env) {
            missing.push(MissingHarnessCommand {
                command: required.to_string(),
                role: MissingHarnessCommandRole::Required,
                detail: err.to_string(),
            });
        }
    }
    let command = plan.command.trim();
    if missing.iter().any(|missing| missing.command == command) {
        return missing;
    }
    if let Err(err) = find_command(command, working_directory, env) {
        missing.push(MissingHarnessCommand {
            command: command.to_string(),
            role: MissingHarnessCommandRole::HarnessCommand,
            detail: err.to_string(),
        });
    }
    missing
}

fn maybe_run_agent_harness_install(
    plan: &AgentHarnessPlan,
    missing: &[MissingHarnessCommand],
    working_directory: Option<&Path>,
    env: &std::collections::BTreeMap<String, String>,
) -> Result<bool> {
    let Some(install) = plan.install.as_ref() else {
        return Ok(false);
    };
    if install.commands.is_empty() {
        return Ok(false);
    }
    if !io::stdin().is_terminal() || !io::stderr().is_terminal() {
        return Ok(false);
    }

    eprintln!("{}", missing_harness_commands_summary(missing));
    if let Some(guidance) = install_guidance(plan) {
        eprintln!();
        eprintln!("{guidance}");
    }
    let question = if install.commands.len() == 1 {
        "Run install command now?"
    } else {
        "Run install commands now?"
    };
    if !prompt_confirm(question, false)? {
        return Ok(false);
    }

    for command in &install.commands {
        run_agent_harness_install_command(command, working_directory, env)?;
    }
    Ok(true)
}

fn run_agent_harness_install_command(
    install: &AgentHarnessInstallCommand,
    working_directory: Option<&Path>,
    env: &std::collections::BTreeMap<String, String>,
) -> Result<()> {
    let mut cmd = if !install.shell.trim().is_empty() {
        shell_command(install.shell.trim())
    } else {
        let command = install.command.trim();
        if command.is_empty() {
            bail!("agent harness install command is required");
        }
        let mut cmd = ProcessCommand::new(command);
        cmd.args(&install.args);
        cmd
    };
    if let Some(working_directory) = working_directory {
        cmd.current_dir(working_directory);
    }
    cmd.env_clear();
    cmd.envs(
        env.iter()
            .map(|(key, value)| (key.as_str(), value.as_str())),
    );
    for (key, value) in &install.env {
        let Some(value) = value.as_str() else {
            bail!("agent harness install env {:?} must be a string", key);
        };
        cmd.env(key, value);
    }
    let display = install_command_display(install);
    let status = cmd
        .stdin(Stdio::inherit())
        .stdout(Stdio::inherit())
        .stderr(Stdio::inherit())
        .status()
        .with_context(|| format!("failed to start agent harness install command: {display}"))?;
    if !status.success() {
        bail!(
            "agent harness install command failed with status {}: {}",
            helper_exit_code(status),
            display
        );
    }
    Ok(())
}

fn shell_command(script: &str) -> ProcessCommand {
    #[cfg(windows)]
    {
        let mut cmd = ProcessCommand::new("cmd");
        cmd.args(["/C", script]);
        cmd
    }
    #[cfg(not(windows))]
    {
        let mut cmd = ProcessCommand::new("sh");
        cmd.args(["-c", script]);
        cmd
    }
}

fn missing_harness_commands_message(
    plan: &AgentHarnessPlan,
    missing: &[MissingHarnessCommand],
) -> String {
    let mut message = missing_harness_commands_summary(missing);
    if let Some(guidance) = install_guidance(plan) {
        message.push_str("\n\n");
        message.push_str(&guidance);
    }
    message
}

fn missing_harness_commands_summary(missing: &[MissingHarnessCommand]) -> String {
    if missing.len() == 1 {
        let missing = &missing[0];
        return match missing.role {
            MissingHarnessCommandRole::Required => format!(
                "required command {:?} is not available: {}",
                missing.command, missing.detail
            ),
            MissingHarnessCommandRole::HarnessCommand => format!(
                "agent harness command {:?} is not available: {}",
                missing.command, missing.detail
            ),
        };
    }
    let mut message = "agent harness commands are not available:".to_string();
    for missing in missing {
        message.push_str(&format!(
            "\n  - {}: {}",
            install_command_quote(&missing.command),
            missing.detail
        ));
    }
    message
}

fn install_guidance(plan: &AgentHarnessPlan) -> Option<String> {
    let install = plan.install.as_ref()?;
    let instructions = install.instructions.trim();
    if instructions.is_empty() && install.commands.is_empty() {
        return None;
    }

    let mut lines = vec![format!(
        "Install instructions for agent harness {:?}:",
        harness_label(plan)
    )];
    if !instructions.is_empty() {
        lines.push(instructions.to_string());
    }
    for command in &install.commands {
        if !command.description.trim().is_empty() {
            lines.push(format!("{}:", command.description.trim()));
        }
        lines.push(format!("  $ {}", install_command_display(command)));
    }
    Some(lines.join("\n"))
}

fn harness_label(plan: &AgentHarnessPlan) -> String {
    if plan.harness.trim().is_empty() {
        plan.provider.clone()
    } else {
        format!("{}/{}", plan.provider, plan.harness)
    }
}

fn install_command_display(command: &AgentHarnessInstallCommand) -> String {
    if !command.shell.trim().is_empty() {
        return command.shell.trim().to_string();
    }
    let mut parts = vec![install_command_quote(command.command.trim())];
    parts.extend(command.args.iter().map(|arg| install_command_quote(arg)));
    parts.join(" ")
}

fn install_command_quote(value: &str) -> String {
    if value.is_empty() {
        return "''".to_string();
    }
    if value
        .bytes()
        .all(|b| b.is_ascii_alphanumeric() || b"@%_+=:,./-".contains(&b))
    {
        return value.to_string();
    }
    format!("'{}'", value.replace('\'', "'\\''"))
}

fn launch_agent_harness(plan: &AgentHarnessPlan, prepared: &PreparedAgentHarness) -> Result<()> {
    let mut cmd = ProcessCommand::new(&prepared.command);
    cmd.args(&plan.args);
    if let Some(working_directory) = &prepared.working_directory {
        cmd.current_dir(working_directory);
    }
    cmd.env_clear();
    cmd.envs(
        prepared
            .env
            .iter()
            .filter_map(|entry| entry.split_once('=')),
    );
    let status = cmd
        .stdin(Stdio::inherit())
        .stdout(Stdio::inherit())
        .stderr(Stdio::inherit())
        .status()
        .with_context(|| format!("failed to start agent harness {:?}", plan.provider))?;
    if status.success() {
        return Ok(());
    }
    std::process::exit(helper_exit_code(status));
}

fn helper_exit_code(status: ExitStatus) -> i32 {
    if let Some(code) = status.code() {
        return code;
    }
    #[cfg(unix)]
    {
        use std::os::unix::process::ExitStatusExt;
        if let Some(signal) = status.signal() {
            return 128 + signal;
        }
    }
    1
}

fn effective_harness_env(
    overlay: &Map<String, Value>,
) -> Result<std::collections::BTreeMap<String, String>> {
    let mut env: std::collections::BTreeMap<String, String> = std::env::vars().collect();
    for (key, value) in overlay {
        let Some(value) = value.as_str() else {
            bail!("agent harness env {:?} must be a string", key);
        };
        env.insert(key.clone(), value.to_string());
    }
    Ok(env)
}

fn env_list(env: std::collections::BTreeMap<String, String>) -> Vec<String> {
    env.into_iter()
        .map(|(key, value)| format!("{key}={value}"))
        .collect()
}

fn find_command(
    command: &str,
    working_directory: Option<&Path>,
    env: &std::collections::BTreeMap<String, String>,
) -> Result<PathBuf> {
    if command.trim().is_empty() {
        bail!("command is required");
    }
    let candidate = PathBuf::from(command);
    if has_path_separator(command) {
        let resolved = if candidate.is_absolute() {
            candidate
        } else if let Some(working_directory) = working_directory {
            working_directory.join(candidate)
        } else {
            std::env::current_dir()
                .context("failed to resolve current directory")?
                .join(candidate)
        };
        if is_executable_file(&resolved) {
            return Ok(resolved);
        }
        bail!("{command} not found");
    }

    let path_value = env.get("PATH").map(String::as_str).unwrap_or("");
    for dir in std::env::split_paths(path_value) {
        let dir = if dir.as_os_str().is_empty() {
            PathBuf::from(".")
        } else {
            dir
        };
        let dir = if !dir.is_absolute() {
            working_directory
                .map(|working_directory| working_directory.join(&dir))
                .unwrap_or(dir)
        } else {
            dir
        };
        let resolved = dir.join(command);
        if is_executable_file(&resolved) {
            return Ok(resolved);
        }
        #[cfg(windows)]
        {
            let resolved = dir.join(format!("{command}.exe"));
            if is_executable_file(&resolved) {
                return Ok(resolved);
            }
        }
    }
    bail!("{command} not found in PATH");
}

fn has_path_separator(command: &str) -> bool {
    command.contains('/')
        || if cfg!(windows) {
            command.contains('\\')
        } else {
            false
        }
}

fn is_executable_file(path: &Path) -> bool {
    let Ok(metadata) = std::fs::metadata(path) else {
        return false;
    };
    if !metadata.is_file() {
        return false;
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        metadata.permissions().mode() & 0o111 != 0
    }
    #[cfg(not(unix))]
    {
        true
    }
}
