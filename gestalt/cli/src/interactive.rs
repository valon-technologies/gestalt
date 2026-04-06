use std::io::{self, BufRead, IsTerminal, Write};

use anyhow::{Context, Result, bail};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PromptOption {
    pub label: String,
    pub detail: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InputPrompt {
    pub label: String,
    pub description: Option<String>,
    pub help_url: Option<String>,
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

pub fn prompt_select(prompt: &str, options: &[PromptOption]) -> Result<usize> {
    if options.is_empty() {
        bail!("no options available");
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
    let mut stderr = io::stderr();
    writeln!(stderr)?;
    writeln!(stderr, "{}", prompt.label)?;
    if let Some(description) = prompt.description.as_deref() {
        writeln!(stderr, "  {description}")?;
    }
    if let Some(help_url) = prompt.help_url.as_deref() {
        writeln!(stderr, "  Help: {help_url}")?;
    }

    loop {
        let value = if prompt.secret && io::stdin().is_terminal() && io::stderr().is_terminal() {
            let prompt_text = match prompt.default.as_deref() {
                Some(default) => format!("Value [{default}]: "),
                None => "Value: ".to_string(),
            };
            rpassword::prompt_password(prompt_text).context("failed to read secret input")?
        } else {
            match prompt.default.as_deref() {
                Some(default) => write!(stderr, "Value [{default}]: ")?,
                None => write!(stderr, "Value: ")?,
            }
            stderr.flush()?;
            read_line()?.context("stdin closed while waiting for input")?
        };

        let trimmed = value.trim().to_string();
        if trimmed.is_empty() {
            if let Some(default) = prompt.default.clone() {
                return Ok(default);
            }
            if !prompt.required {
                return Ok(String::new());
            }
            writeln!(stderr, "A value is required.")?;
            continue;
        }

        return Ok(trimmed);
    }
}

pub fn prompt_confirm(question: &str, default: bool) -> Result<bool> {
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
