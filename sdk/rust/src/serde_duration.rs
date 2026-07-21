use std::time::Duration;

pub fn serialize<S>(value: &Option<Duration>, serializer: S) -> Result<S::Ok, S::Error>
where
    S: serde::Serializer,
{
    match value {
        Some(d) => {
            let seconds = d.as_secs() as i64;
            let nanos = d.subsec_nanos() as i32;
            let total_nanos = seconds as i128 * 1_000_000_000i128 + nanos as i128;
            if total_nanos == 0 {
                return serializer.serialize_str("0s");
            }
            let abs = total_nanos.unsigned_abs();
            let secs = abs / 1_000_000_000;
            let frac = abs % 1_000_000_000;
            if frac == 0 {
                serializer.serialize_str(&format!("{}s", secs))
            } else {
                let fractional = format!("{:09}", frac);
                let fractional = fractional.trim_end_matches('0');
                serializer.serialize_str(&format!("{}.{}s", secs, fractional))
            }
        }
        None => serializer.serialize_none(),
    }
}

pub fn deserialize<'de, D>(deserializer: D) -> Result<Option<Duration>, D::Error>
where
    D: serde::Deserializer<'de>,
{
    let opt: Option<String> = serde::Deserialize::deserialize(deserializer)?;
    match opt {
        Some(s) => {
            let s = s
                .strip_suffix('s')
                .ok_or_else(|| serde::de::Error::custom("duration must end with 's'"))?;
            let (secs_str, nanos_str) = s.split_once('.').unwrap_or((s, ""));
            let seconds: i64 = secs_str.parse().map_err(serde::de::Error::custom)?;
            let mut nanos: i32 = if nanos_str.is_empty() {
                0
            } else {
                let padded = format!("{:0<9}", nanos_str);
                padded.parse().map_err(serde::de::Error::custom)?
            };
            if seconds < 0 && nanos > 0 {
                nanos = -nanos;
            }
            let d = Duration::new(seconds.unsigned_abs(), nanos.unsigned_abs());
            Ok(Some(if seconds >= 0 {
                d
            } else {
                // Negative durations don't have a clean std::time::Duration representation;
                // saturate to zero.
                if seconds < 0 { Duration::ZERO } else { d }
            }))
        }
        None => Ok(None),
    }
}
