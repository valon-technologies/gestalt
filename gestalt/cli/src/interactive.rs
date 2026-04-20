use std::io::{self, BufRead, IsTerminal, Write};

use anyhow::{Context, Result, bail};
use dialoguer::{Confirm, Input, Password, Select, console::Term, theme::ColorfulTheme};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PromptOption {
    pub label: String,
    pub detail: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InputPrompt {
    pub label: String,
    pub description: Option<String>,
    pub default: Option<String>,
    pub required: bool,
    pub secret: bool,
}

fn read_line() -> Result<Option<String>> {
    let stdin = io::stdin();
    let mut lines = stdin.lock().lines();
    match lines.next() {
        Some(line) => Ok(Some(
            line.context("failed to read input")?.trim().to_string(),
        )),
        None => Ok(None),
    }
}

fn use_terminal_prompts() -> bool {
    io::stdin().is_terminal() && io::stderr().is_terminal()
}

fn render_prompt_header(prompt: &InputPrompt) -> Result<()> {
    let mut stderr = io::stderr();
    writeln!(stderr)?;
    writeln!(stderr, "{}", prompt.label)?;
    if let Some(description) = prompt.description.as_deref() {
        writeln!(stderr, "  {description}")?;
    }
    Ok(())
}

fn finalize_prompt_value(prompt: &InputPrompt, value: String) -> Result<Option<String>> {
    let trimmed = value.trim().to_string();
    if trimmed.is_empty() {
        if let Some(default) = prompt.default.clone() {
            return Ok(Some(default));
        }
        if !prompt.required {
            return Ok(Some(String::new()));
        }

        let mut stderr = io::stderr();
        writeln!(stderr, "A value is required.")?;
        return Ok(None);
    }

    Ok(Some(trimmed))
}

pub fn prompt_select(prompt: &str, options: &[PromptOption]) -> Result<usize> {
    if options.is_empty() {
        bail!("no options available");
    }

    if use_terminal_prompts() {
        let items: Vec<String> = options
            .iter()
            .map(|option| match option.detail.as_deref() {
                Some(detail) => format!("{}\n    {detail}", option.label),
                None => option.label.clone(),
            })
            .collect();
        return Select::with_theme(&ColorfulTheme::default())
            .with_prompt(prompt)
            .items(&items)
            .default(0)
            .interact_on(&Term::stderr())
            .context("failed to read selection");
    }

    let mut stderr = io::stderr();
    writeln!(stderr, "{prompt}")?;
    for (idx, option) in options.iter().enumerate() {
        writeln!(stderr, "  {}. {}", idx + 1, option.label)?;
        if let Some(detail) = option.detail.as_deref() {
            writeln!(stderr, "     {detail}")?;
        }
    }

    loop {
        write!(stderr, "Selection [1-{}]: ", options.len())?;
        stderr.flush()?;
        let input = read_line()?.context("stdin closed while waiting for selection")?;

        if let Ok(choice) = input.parse::<usize>()
            && (1..=options.len()).contains(&choice)
        {
            return Ok(choice - 1);
        }

        writeln!(stderr, "Enter a number between 1 and {}.", options.len())?;
    }
}

pub fn prompt_input(prompt: &InputPrompt) -> Result<String> {
    render_prompt_header(prompt)?;

    loop {
        let value = if use_terminal_prompts() {
            if prompt.secret {
                let prompt_text = match prompt.default.as_deref() {
                    Some(default) => format!("Value [{default}]"),
                    None => "Value".to_string(),
                };
                let theme = ColorfulTheme::default();
                Password::with_theme(&theme)
                    .with_prompt(prompt_text)
                    .allow_empty_password(true)
                    .interact_on(&Term::stderr())
                    .context("failed to read secret input")?
            } else {
                let theme = ColorfulTheme::default();
                let mut input = Input::<String>::with_theme(&theme)
                    .with_prompt("Value")
                    .allow_empty(true);
                if let Some(default) = prompt.default.clone() {
                    input = input.default(default);
                }
                input
                    .interact_text_on(&Term::stderr())
                    .context("failed to read input")?
            }
        } else {
            let mut stderr = io::stderr();
            match prompt.default.as_deref() {
                Some(default) => write!(stderr, "Value [{default}]: ")?,
                None => write!(stderr, "Value: ")?,
            }
            stderr.flush()?;
            read_line()?.context("stdin closed while waiting for input")?
        };

        if let Some(value) = finalize_prompt_value(prompt, value)? {
            return Ok(value);
        }
    }
}

pub fn prompt_confirm(question: &str, default: bool) -> Result<bool> {
    if use_terminal_prompts() {
        return Confirm::with_theme(&ColorfulTheme::default())
            .with_prompt(question)
            .default(default)
            .interact_on(&Term::stderr())
            .context("failed to read confirmation");
    }

    let hint = if default { "Y/n" } else { "y/N" };
    let mut stderr = io::stderr();
    write!(stderr, "{} [{}]: ", question, hint)?;
    stderr.flush()?;

    let input = read_line()?
        .context("stdin closed while waiting for confirmation")?
        .to_lowercase();
    if input.is_empty() {
        Ok(default)
    } else {
        Ok(input.starts_with('y'))
    }
}
