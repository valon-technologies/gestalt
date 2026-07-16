//! Recording-transport tests for the generated public App client.

use std::sync::{Arc, Mutex};

use base64::Engine;
use gestalt::proto;
use gestalt::public::generated::app::AppInvokeRequest;
use gestalt::public::generated::app_client::AppClient;
use gestalt::public::generated::invoke_support::InvokeError;
use gestalt::public::generated::rpc_support::GestaltError;
use gestalt::public::generated::unary_transport::UnaryTransport;
use prost::Message;
use serde_json::{Value, json};

#[derive(Clone)]
struct RecordingTransport {
    state: Arc<Mutex<RecordingState>>,
}

struct RecordingState {
    calls: usize,
    err_code: Option<i32>,
    err_message: Option<String>,
    body: Vec<u8>,
    last_request: Option<Vec<u8>>,
}

impl UnaryTransport for RecordingTransport {
    fn unary<Req, Resp>(
        &self,
        method: &gestalt::public::generated::metadata::Method,
        request: &Req,
        response: &mut Resp,
    ) -> impl std::future::Future<Output = Result<(), GestaltError>> + Send
    where
        Req: Message + Send + Sync,
        Resp: Message + Default + Send,
    {
        let state = Arc::clone(&self.state);
        async move {
            let mut guard = state.lock().expect("lock");
            guard.calls += 1;
            guard.last_request = Some(request.encode_to_vec());
            if method.name != "Invoke" {
                return Err(GestaltError::new(2, "unexpected method"));
            }
            if let (Some(code), Some(message)) = (guard.err_code, guard.err_message.clone()) {
                return Err(GestaltError::new(code, message));
            }
            let out = proto::v1::OperationResult {
                status: 200,
                body: guard.body.clone(),
                ..Default::default()
            };
            let bytes = out.encode_to_vec();
            *response = Resp::decode(bytes.as_slice())
                .map_err(|err| GestaltError::new(3, err.to_string()))?;
            Ok(())
        }
    }
}

fn load_cases() -> Value {
    let path = format!(
        "{}/../testdata/public_conformance/client_cases.json",
        env!("CARGO_MANIFEST_DIR")
    );
    let data = std::fs::read_to_string(path).expect("read client_cases.json");
    serde_json::from_str(&data).expect("parse client_cases.json")
}

#[tokio::test]
async fn public_app_client_shared_cases() {
    let cases = load_cases().as_array().expect("case array").clone();
    for case in cases {
        let id = case["id"].as_str().expect("id");
        let state = Arc::new(Mutex::new(RecordingState {
            calls: 0,
            err_code: None,
            err_message: None,
            body: Vec::new(),
            last_request: None,
        }));
        match id {
            "invoke_success" => {
                let body_b64 = case["response"]["operationResult"]["bodyBase64"]
                    .as_str()
                    .expect("bodyBase64");
                state.lock().expect("lock").body = base64::engine::general_purpose::STANDARD
                    .decode(body_b64)
                    .expect("decode");
            }
            "platform_error" => {
                let err = &case["response"]["gestaltError"];
                let mut guard = state.lock().expect("lock");
                guard.err_code = Some(err["code"].as_i64().expect("code") as i32);
                guard.err_message = Some(err["message"].as_str().expect("message").to_string());
            }
            other => panic!("unknown case {other}"),
        }

        let transport = RecordingTransport {
            state: Arc::clone(&state),
        };
        let client = AppClient::new(transport);
        let public = &case["publicRequest"];
        let request = AppInvokeRequest {
            app: public["app"].as_str().expect("app").to_string(),
            operation: public["operation"].as_str().expect("operation").to_string(),
            params: public
                .get("params")
                .cloned()
                .and_then(|v| v.as_object().cloned()),
            ..Default::default()
        };

        if id == "invoke_success" {
            let got = client.invoke(request).await.expect("invoke");
            assert_eq!(got, case["expect"]["result"]);
        } else {
            let err = client.invoke(request).await.expect_err("platform error");
            assert!(matches!(err, InvokeError::Transport(_)));
            if let InvokeError::Transport(gerr) = err {
                assert_eq!(
                    gerr.code,
                    case["expect"]["gestaltError"]["code"].as_i64().unwrap() as i32
                );
                assert_eq!(
                    gerr.message,
                    case["expect"]["gestaltError"]["message"].as_str().unwrap()
                );
            }
        }

        let guard = state.lock().expect("lock");
        let encoded = guard
            .last_request
            .as_ref()
            .expect("transport did not receive a request");
        let wire: proto::v1::AppInvokeRequest =
            proto::v1::AppInvokeRequest::decode(encoded.as_slice()).expect("decode request");
        let got_wire = wire_request_json(&wire, &case["wireRequest"]);
        assert_eq!(got_wire, case["wireRequest"]);
        assert_eq!(
            guard.calls,
            case["expect"]["calls"].as_u64().unwrap() as usize
        );
    }
}

fn wire_request_json(wire: &proto::v1::AppInvokeRequest, expected: &Value) -> Value {
    let mut got = json!({
        "app": wire.app,
        "operation": wire.operation,
    });
    if expected.get("params").is_some() {
        got["params"] = match &wire.params {
            Some(params) => prost_struct_to_json(params),
            None => Value::Null,
        };
    }
    got
}

fn prost_struct_to_json(value: &prost_types::Struct) -> Value {
    let mut map = serde_json::Map::new();
    for (key, field) in &value.fields {
        map.insert(key.clone(), prost_value_to_json(field));
    }
    Value::Object(map)
}

fn prost_value_to_json(value: &prost_types::Value) -> Value {
    use prost_types::value::Kind;
    match &value.kind {
        Some(Kind::NullValue(_)) => Value::Null,
        Some(Kind::BoolValue(v)) => json!(*v),
        Some(Kind::NumberValue(v)) => json!(*v),
        Some(Kind::StringValue(v)) => json!(v),
        Some(Kind::StructValue(v)) => prost_struct_to_json(v),
        Some(Kind::ListValue(list)) => {
            Value::Array(list.values.iter().map(prost_value_to_json).collect())
        }
        None => Value::Null,
    }
}
