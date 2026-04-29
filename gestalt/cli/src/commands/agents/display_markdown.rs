pub(super) fn effective_format<'a>(format: Option<&'a str>, language: Option<&str>) -> &'a str {
    match format {
        Some(format) => format,
        None if language.is_some() => "code",
        None => "plain",
    }
}

pub(super) fn plain_text_for_format(
    text: &str,
    format: Option<&str>,
    language: Option<&str>,
) -> String {
    if is_markdown_format(effective_format(format, language)) {
        markdown_to_plain_text(text)
    } else {
        text.to_string()
    }
}

pub(super) fn is_markdown_format(format: &str) -> bool {
    matches!(format.trim(), "markdown" | "md")
}

pub(super) fn is_code_like_format(format: &str) -> bool {
    matches!(format.trim(), "code" | "json" | "diff")
}

pub(super) fn code_fence_language(text: &str) -> Option<&str> {
    text.trim_start().strip_prefix("```").map(str::trim)
}

pub(super) fn markdown_link(rest: &str) -> Option<(&str, &str, usize)> {
    if !rest.starts_with('[') {
        return None;
    }
    let label_end = 1 + rest[1..].find("](")?;
    let url_start = label_end + 2;
    let url_end = url_start + rest[url_start..].find(')')?;
    let label = &rest[1..label_end];
    if label.is_empty() {
        return None;
    }
    let url = &rest[url_start..url_end];
    if url.is_empty() {
        return None;
    }
    Some((label, url, url_end + 1))
}

pub(super) fn raw_url(rest: &str) -> Option<(&str, usize)> {
    if !(rest.starts_with("https://") || rest.starts_with("http://")) {
        return None;
    }
    let end = rest
        .char_indices()
        .find_map(|(index, ch)| ch.is_whitespace().then_some(index))
        .unwrap_or(rest.len());
    Some((&rest[..end], end))
}

pub(super) fn delimited<'a>(
    text: &'a str,
    index: usize,
    delimiter: &str,
) -> Option<(&'a str, usize)> {
    let rest = &text[index..];
    if !rest.starts_with(delimiter) {
        return None;
    }
    if delimiter != "`" && !delimiter_boundary_before(text, index) {
        return None;
    }
    let after_open = &rest[delimiter.len()..];
    if after_open.chars().next().is_none_or(char::is_whitespace) {
        return None;
    }
    let close = after_open.find(delimiter)?;
    if close == 0 {
        return None;
    }
    let inner = &after_open[..close];
    if inner.chars().last().is_none_or(char::is_whitespace) {
        return None;
    }
    let consumed = delimiter.len() + close + delimiter.len();
    if delimiter != "`" && !delimiter_boundary_after(text, index + consumed) {
        return None;
    }
    Some((inner, consumed))
}

fn markdown_to_plain_text(text: &str) -> String {
    let mut rendered = String::new();
    let mut in_code_fence = false;
    for line in text.split('\n') {
        if code_fence_language(line).is_some() {
            in_code_fence = !in_code_fence;
            continue;
        }
        if !rendered.is_empty() {
            rendered.push('\n');
        }
        if in_code_fence {
            rendered.push_str(line);
        } else {
            rendered.push_str(&markdown_inline_to_plain_text(line));
        }
    }
    rendered
}

fn markdown_inline_to_plain_text(text: &str) -> String {
    let mut rendered = String::new();
    let mut index = 0usize;
    while index < text.len() {
        let rest = &text[index..];

        if let Some((label, url, consumed)) = markdown_link(rest) {
            rendered.push_str(&markdown_inline_to_plain_text(label));
            rendered.push_str(" (");
            rendered.push_str(url);
            rendered.push(')');
            index += consumed;
            continue;
        }

        if let Some((url, consumed)) = raw_url(rest) {
            rendered.push_str(url);
            index += consumed;
            continue;
        }

        if let Some((inner, consumed)) = delimited(text, index, "`") {
            rendered.push_str(inner);
            index += consumed;
            continue;
        }

        if let Some((inner, consumed)) = delimited(text, index, "**") {
            rendered.push_str(&markdown_inline_to_plain_text(inner));
            index += consumed;
            continue;
        }

        if let Some((inner, consumed)) =
            delimited(text, index, "*").or_else(|| delimited(text, index, "_"))
        {
            rendered.push_str(&markdown_inline_to_plain_text(inner));
            index += consumed;
            continue;
        }

        let Some(ch) = rest.chars().next() else {
            break;
        };
        rendered.push(ch);
        index += ch.len_utf8();
    }
    rendered
}

fn delimiter_boundary_before(text: &str, index: usize) -> bool {
    text[..index]
        .chars()
        .next_back()
        .is_none_or(|ch| !is_identifier_char(ch))
}

fn delimiter_boundary_after(text: &str, index: usize) -> bool {
    text[index..]
        .chars()
        .next()
        .is_none_or(|ch| !is_identifier_char(ch))
}

fn is_identifier_char(ch: char) -> bool {
    ch.is_alphanumeric() || ch == '_'
}
