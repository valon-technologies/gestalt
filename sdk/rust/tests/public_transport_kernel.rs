use base64::Engine as _;
use base64::engine::general_purpose::STANDARD as BASE64;
use gestalt::proto::v1;
use gestalt::public::generated::metadata::{METHOD_APP_INVOKE, Method, PublicField};
use gestalt::public::generated::transport_kernel::{
    RawRestResponse, build_rest_body, build_rest_path, build_rest_query, decode_rest_response,
    parse_gateway_error,
};
use prost::Message as _;
use serde::Deserialize;
use serde_json::{Map, Value};
use std::fs;
use std::path::PathBuf;

#[derive(Deserialize)]
struct TransportKernelCase {
    id: String,
    request: Option<Value>,
    #[serde(rename = "overrideQueryFields")]
    override_query_fields: Option<Vec<PublicFieldFixture>>,
    #[serde(rename = "overrideHttpBody")]
    override_http_body: Option<String>,
    #[serde(rename = "expectPrepare")]
    expect_prepare: Option<PrepareExpect>,
    #[serde(rename = "expectDecode")]
    expect_decode: Option<DecodeExpect>,
    #[serde(rename = "expectGatewayError")]
    expect_gateway_error: Option<GatewayErrorExpect>,
    #[serde(rename = "rawResponse")]
    raw_response: Option<RawResponseFixture>,
    #[serde(rename = "expectGestaltError")]
    expect_gestalt_error: Option<GestaltErrorExpect>,
}

#[derive(Deserialize)]
struct PublicFieldFixture {
    name: String,
    #[serde(rename = "jsonName")]
    json_name: String,
}

#[derive(Deserialize)]
struct PrepareExpect {
    verb: String,
    path: String,
    query: Vec<[String; 2]>,
    body: Option<Value>,
}

#[derive(Deserialize)]
struct DecodeExpect {
    status: i32,
    #[serde(rename = "bodyBase64")]
    body_base64: String,
    #[serde(rename = "headerKeys")]
    header_keys: Option<Vec<String>>,
    #[serde(rename = "headerValueCounts")]
    header_value_counts: Option<Map<String, Value>>,
}

#[derive(Deserialize)]
struct GatewayErrorExpect {
    code: i32,
    message: Option<String>,
}

#[derive(Deserialize)]
struct GestaltErrorExpect {
    code: i32,
}

#[derive(Deserialize)]
struct RawResponseFixture {
    status: u16,
    #[serde(rename = "bodyText")]
    body_text: Option<String>,
    #[serde(rename = "bodyBase64")]
    body_base64: Option<String>,
    headers: Option<Vec<[String; 2]>>,
}

fn fixture_path() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../testdata/public_conformance/transport_kernel_cases.json")
}

fn load_cases() -> Vec<TransportKernelCase> {
    let raw = fs::read_to_string(fixture_path()).expect("read fixture");
    serde_json::from_str(&raw).expect("decode fixture")
}

#[test]
fn fixture_cases_are_covered() {
    for case in load_cases() {
        let covered = case.expect_prepare.is_some()
            || case.expect_decode.is_some()
            || case.expect_gateway_error.is_some()
            || case.expect_gestalt_error.is_some();
        assert!(covered, "case {} has no expectations", case.id);
    }
}

#[test]
fn prepare_cases_from_fixture() {
    for case in load_cases() {
        let Some(ref expect) = case.expect_prepare else {
            continue;
        };
        let Some(ref request) = case.request else {
            continue;
        };
        let method = method_for_prepare_case(&case);
        let path = build_rest_path(&method, request).expect(&case.id);
        let query = build_rest_query(&method, request);
        let body = build_rest_body(&method, request);
        assert_eq!(method.http_verb, expect.verb, "{}", case.id);
        assert_eq!(path, expect.path, "{}", case.id);
        let want_query: Vec<(String, String)> = expect
            .query
            .iter()
            .map(|pair| (pair[0].clone(), pair[1].clone()))
            .collect();
        let mut query = query;
        query.sort();
        let mut want_query = want_query;
        want_query.sort();
        assert_eq!(query, want_query, "{}", case.id);
        match (body, &expect.body) {
            (None, None) => {}
            (Some(body), Some(want)) => assert_eq!(&body, want, "{}", case.id),
            (got, want) => panic!("case {} body mismatch: {:?} vs {:?}", case.id, got, want),
        }
    }
}

#[test]
fn decode_cases_from_fixture() {
    for case in load_cases() {
        let Some(expect) = case.expect_decode else {
            continue;
        };
        let Some(raw) = case.raw_response else {
            continue;
        };
        let bytes = decode_rest_response(&METHOD_APP_INVOKE, raw_response_from_fixture(&raw))
            .expect(&case.id);
        let response = v1::OperationResult::decode(bytes.as_slice()).expect(&case.id);
        assert_eq!(response.status, expect.status, "{}", case.id);
        assert_eq!(
            response.body,
            BASE64.decode(expect.body_base64).expect("decode body"),
            "{}",
            case.id
        );
        for key in expect.header_keys.unwrap_or_default() {
            assert!(response.headers.contains_key(&key), "{}", case.id);
        }
        for (key, count) in expect.header_value_counts.unwrap_or_default() {
            let want = count.as_u64().expect("count") as usize;
            assert_eq!(
                response.headers.get(&key).map(|v| v.values.len()),
                Some(want),
                "{}",
                case.id
            );
        }
    }
}

#[test]
fn gateway_cases_from_fixture() {
    for case in load_cases() {
        let Some(expect) = case.expect_gateway_error else {
            continue;
        };
        let Some(raw) = case.raw_response else {
            continue;
        };
        let err = parse_gateway_error(raw.status, &raw_body(&raw));
        assert_eq!(err.code, expect.code, "{}", case.id);
        if let Some(message) = expect.message {
            assert_eq!(err.message, message, "{}", case.id);
        }
    }
}

#[test]
fn gestalt_error_cases_from_fixture() {
    for case in load_cases() {
        let Some(expect) = case.expect_gestalt_error else {
            continue;
        };
        let Some(raw) = case.raw_response else {
            continue;
        };
        let err = decode_rest_response(&METHOD_APP_INVOKE, raw_response_from_fixture(&raw))
            .expect_err(&case.id);
        assert_eq!(err.code, expect.code, "{}", case.id);
    }
}

fn method_for_prepare_case(case: &TransportKernelCase) -> Method {
    if case.override_query_fields.is_none() && case.override_http_body.is_none() {
        return METHOD_APP_INVOKE.clone();
    }
    let mut method = METHOD_APP_INVOKE.clone();
    if let Some(fields) = &case.override_query_fields {
        let extra: Vec<PublicField> = fields
            .iter()
            .map(|field| PublicField {
                name: leak_str(field.name.clone()),
                json_name: leak_str(field.json_name.clone()),
            })
            .collect();
        let combined: Vec<PublicField> = method
            .http_query_fields
            .iter()
            .cloned()
            .chain(extra)
            .collect();
        method.http_query_fields = leak_slice(combined);
    }
    if let Some(body) = &case.override_http_body {
        method.http_body = leak_str(body.clone());
    }
    method
}

fn leak_str(value: String) -> &'static str {
    Box::leak(value.into_boxed_str())
}

fn leak_slice(values: Vec<PublicField>) -> &'static [PublicField] {
    Box::leak(values.into_boxed_slice())
}

fn raw_response_from_fixture(raw: &RawResponseFixture) -> RawRestResponse {
    RawRestResponse {
        status: raw.status,
        headers: raw
            .headers
            .clone()
            .unwrap_or_default()
            .into_iter()
            .map(|pair| (pair[0].clone(), pair[1].clone()))
            .collect(),
        body: raw_body(raw),
    }
}

fn raw_body(raw: &RawResponseFixture) -> Vec<u8> {
    if let Some(text) = &raw.body_text {
        return text.as_bytes().to_vec();
    }
    if let Some(encoded) = &raw.body_base64 {
        return BASE64.decode(encoded).expect("decode body");
    }
    Vec::new()
}
