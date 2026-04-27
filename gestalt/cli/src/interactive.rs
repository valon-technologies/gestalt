use std::io::{self, BufRead, IsTerminal, Write};
use std::path::PathBuf;

use anyhow::{Context, Result, bail};
use rustyline::error::ReadlineError;
use rustyline::{Config, DefaultEditor};

use crate::paths;

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

pub enum PromptLine {
    Line(String),
    Interrupted,
    Eof,
}

enum LineReaderBackend {
    Editor {
        editor: Box<DefaultEditor>,
        history_path: Option<PathBuf>,
    },
    Stdin,
}

pub struct InteractiveLineReader {
    backend: LineReaderBackend,
}

impl InteractiveLineReader {
    pub fn with_history_namespace(namespace: &str) -> Result<Self> {
        if !io::stdin().is_terminal() || !io::stderr().is_terminal() {
            return Ok(Self {
                backend: LineReaderBackend::Stdin,
            });
        }

        // Keep SIGINT owned by the CLI cancellation handler while line editing is active.
        let editor_config = Config::builder().enable_signals(false).build();
        let mut editor = match DefaultEditor::with_config(editor_config) {
            Ok(editor) => editor,
            Err(err) => {
                eprintln!("warning: failed to initialize interactive line editor: {err}");
                return Ok(Self {
                    backend: LineReaderBackend::Stdin,
                });
            }
        };
        let history_path = match history_path(namespace) {
            Ok(path) => Some(path),
            Err(err) => {
                eprintln!("warning: failed to resolve interactive history path: {err}");
                None
            }
        };
        if let Some(path) = history_path.as_ref()
            && path.exists()
            && let Err(err) = editor.load_history(path)
        {
            eprintln!("warning: failed to load history {}: {err}", path.display());
        }

        Ok(Self {
            backend: LineReaderBackend::Editor {
                editor: Box::new(editor),
                history_path,
            },
        })
    }

    pub fn read_line(&mut self, prompt: &str) -> Result<PromptLine> {
        match &mut self.backend {
            LineReaderBackend::Editor { editor, .. } => match editor.readline(prompt) {
                Ok(line) => {
                    if !line.trim().is_empty() {
                        let _ = editor.add_history_entry(line.as_str());
                    }
                    Ok(PromptLine::Line(line))
                }
                Err(ReadlineError::Interrupted) => Ok(PromptLine::Interrupted),
                Err(ReadlineError::Eof) => Ok(PromptLine::Eof),
                Err(err) => Err(err).context("failed to read interactive input"),
            },
            LineReaderBackend::Stdin => read_prompt_line(prompt),
        }
    }

    pub fn save_history(&mut self) -> Result<()> {
        match &mut self.backend {
            LineReaderBackend::Editor {
                editor,
                history_path,
            } => {
                let Some(history_path) = history_path.as_ref() else {
                    return Ok(());
                };
                if let Some(parent) = history_path.parent() {
                    std::fs::create_dir_all(parent).with_context(|| {
                        format!("failed to create history directory {}", parent.display())
                    })?;
                }
                editor
                    .save_history(history_path)
                    .with_context(|| format!("failed to save history {}", history_path.display()))
            }
            LineReaderBackend::Stdin => Ok(()),
        }
    }
}

impl Drop for InteractiveLineReader {
    fn drop(&mut self) {
        let _ = self.save_history();
    }
}

fn history_path(namespace: &str) -> Result<PathBuf> {
    let cwd = std::env::current_dir().context("failed to determine current directory")?;
    let encoded_cwd: String =
        url::form_urlencoded::byte_serialize(cwd.to_string_lossy().as_bytes()).collect();
    let namespace = namespace
        .trim_matches(|ch: char| ch == '/' || ch.is_whitespace())
        .replace('/', "_");
    let namespace = if namespace.is_empty() {
        "default".to_string()
    } else {
        namespace
    };
    Ok(paths::gestalt_config_dir()?
        .join("history")
        .join(namespace)
        .join(format!("{encoded_cwd}.history")))
}

fn read_prompt_line(prompt: &str) -> Result<PromptLine> {
    let mut stderr = io::stderr().lock();
    write!(stderr, "{prompt}")?;
    stderr.flush()?;

    let mut line = String::new();
    let read = io::stdin()
        .read_line(&mut line)
        .context("failed to read input")?;
    if read == 0 {
        writeln!(stderr)?;
        return Ok(PromptLine::Eof);
    }
    Ok(PromptLine::Line(
        line.trim_end_matches(['\r', '\n']).to_string(),
    ))
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
