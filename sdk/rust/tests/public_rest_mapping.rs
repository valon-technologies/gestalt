use gestalt::public::rest_mapping::{build_body_map, build_query_pairs, encode_query_string};
use serde_json::{Map, json};

#[test]
fn build_query_pairs_nested_and_repeated() {
    let mut request = Map::new();
    request.insert("app".into(), json!("example"));
    request.insert("tags".into(), json!(["a", "b"]));
    request.insert(
        "filter".into(),
        json!({"status": "open", "nested": {"x": 1}}),
    );
    let path_fields = vec![gestalt::public::generated::metadata::PublicField {
        name: "app",
        json_name: "app",
    }];
    let pairs = build_query_pairs(&request, &path_fields);
    assert!(pairs.iter().all(|(key, _)| key != "app"));
    assert!(pairs.contains(&("tags".into(), "a".into())));
    assert!(pairs.contains(&("tags".into(), "b".into())));
    assert!(pairs.contains(&("filter.status".into(), "open".into())));
    assert!(pairs.contains(&("filter.nested.x".into(), "1".into())));
}

#[test]
fn build_body_map_excludes_path_fields() {
    let mut request = Map::new();
    request.insert("app".into(), json!("example"));
    request.insert("operation".into(), json!("sync"));
    request.insert("params".into(), json!({}));
    let path_fields = vec![
        gestalt::public::generated::metadata::PublicField {
            name: "app",
            json_name: "app",
        },
        gestalt::public::generated::metadata::PublicField {
            name: "operation",
            json_name: "operation",
        },
    ];
    let body = build_body_map(&request, &path_fields);
    assert_eq!(body.get("params").cloned(), Some(json!({})));
    assert!(!body.contains_key("app"));
    assert!(!body.contains_key("operation"));
}

#[test]
fn encode_query_string_percent_encodes() {
    let encoded = encode_query_string(&[("a/b".into(), "c d".into())]);
    assert_eq!(encoded, "a%2Fb=c%20d");
}
