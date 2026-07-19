use serde_json::{Map, Value};

pub(super) fn target_app<'a>(value: &'a Value, preferred_app: Option<&str>) -> Option<&'a str> {
    target_app_step(value, preferred_app)?
        .get("name")
        .and_then(Value::as_str)
}

pub(super) fn target_operation<'a>(
    value: &'a Value,
    preferred_app: Option<&str>,
) -> Option<&'a str> {
    target_app_step(value, preferred_app)?
        .get("operation")
        .and_then(Value::as_str)
}

fn target_app_step<'a>(
    value: &'a Value,
    preferred_app: Option<&str>,
) -> Option<&'a Map<String, Value>> {
    let steps = value.get("target")?.get("steps")?.as_array()?;
    let mut first_app_step = None;
    for step in steps {
        let Some(app_step) = step
            .get("action")
            .and_then(|a| a.get("App"))
            .or_else(|| step.get("app"))
            .and_then(Value::as_object)
        else {
            continue;
        };
        if first_app_step.is_none() {
            first_app_step = Some(app_step);
        }
        if preferred_app.is_some_and(|preferred_app| {
            app_step.get("name").and_then(Value::as_str) == Some(preferred_app)
        }) {
            return Some(app_step);
        }
    }
    first_app_step
}

#[cfg(test)]
mod tests {
    use serde_json::json;

    use super::*;

    #[test]
    fn target_app_fields_prefer_matching_app_step() {
        let value = json!({
            "target": {
                "steps": [
                    {
                        "id": "first",
                        "app": {
                            "name": "github",
                            "operation": "createIssue"
                        }
                    },
                    {
                        "id": "second",
                        "app": {
                            "name": "slack",
                            "operation": "reply"
                        }
                    }
                ]
            }
        });

        assert_eq!(target_app(&value, None), Some("github"));
        assert_eq!(target_operation(&value, None), Some("createIssue"));
        assert_eq!(target_app(&value, Some("slack")), Some("slack"));
        assert_eq!(target_operation(&value, Some("slack")), Some("reply"));
        assert_eq!(target_app(&value, Some("jira")), Some("github"));
        assert_eq!(target_operation(&value, Some("jira")), Some("createIssue"));
    }

    #[test]
    fn target_app_fields_skip_agent_steps() {
        let value = json!({
            "target": {
                "steps": [
                    {
                        "id": "summarize",
                        "agent": {
                            "provider": "simple"
                        }
                    },
                    {
                        "id": "notify",
                        "app": {
                            "name": "slack",
                            "operation": "reply"
                        }
                    }
                ]
            }
        });

        assert_eq!(target_app(&value, None), Some("slack"));
        assert_eq!(target_operation(&value, None), Some("reply"));
    }
}
