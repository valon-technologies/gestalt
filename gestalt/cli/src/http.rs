use std::io::{self, Write};

use reqwest::{StatusCode, header};

pub const APPLICATION_JSON: &str = "application/json";
pub const APPLICATION_X_WWW_FORM_URLENCODED: &str = "application/x-www-form-urlencoded";
pub const TEXT_HTML: &str = "text/html";
pub const TEXT_PLAIN: &str = "text/plain";
pub const CACHE_CONTROL_NO_STORE: &str = "no-store";

const CONNECTION_CLOSE: &str = "close";
const UTF_8: &str = "utf-8";

pub fn write_response<W: Write>(
    mut writer: W,
    status: StatusCode,
    content_type: &str,
    body: &str,
    extra_headers: &[(&str, &str)],
) -> io::Result<()> {
    write!(
        writer,
        "HTTP/1.1 {} {}\r\n{}: {}; charset={}\r\n{}: {}\r\n{}: {}\r\n",
        status.as_u16(),
        status.canonical_reason().unwrap_or("Unknown"),
        header::CONTENT_TYPE.as_str(),
        content_type,
        UTF_8,
        header::CONTENT_LENGTH.as_str(),
        body.len(),
        header::CONNECTION.as_str(),
        CONNECTION_CLOSE,
    )?;

    for (name, value) in extra_headers {
        write!(writer, "{}: {}\r\n", name, value)?;
    }

    write!(writer, "\r\n{body}")
}
