#[test]
fn well_known_helpers_round_trip_native_values() -> gestalt::Result<()> {
    let message = gestalt::protocol::struct_from_json(serde_json::json!({
        "b": "two",
        "a": true,
    }))?;

    assert_eq!(
        gestalt::protocol::json_from_struct(&message),
        serde_json::json!({
            "a": true,
            "b": "two",
        })
    );

    Ok(())
}
