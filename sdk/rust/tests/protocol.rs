#[test]
fn protocol_boundary_helpers_round_trip_binary() -> gestalt::Result<()> {
    let message = gestalt::protocol::struct_from_json(serde_json::json!({
        "b": "two",
        "a": true,
    }))?;

    let first = gestalt::protocol::marshal_proto_deterministic(&message);
    let second = gestalt::protocol::marshal_proto_deterministic(&message);

    assert_eq!(first, second);

    let decoded: gestalt::protocol::Struct = gestalt::protocol::unmarshal_proto(first)?;
    assert_eq!(
        gestalt::protocol::json_from_struct(&decoded),
        serde_json::json!({
            "a": true,
            "b": "two",
        })
    );

    Ok(())
}
