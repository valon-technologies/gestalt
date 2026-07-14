use gestalt::{
    build_workflow_from_lowering_case, canonical_workflow_definition_spec, define_workflow,
    event, load_workflow_lowering_contract, resolve_workflow_definition_spec,
    resolve_workflow_definition_spec_from_builder, schedule, DefineWorkflowOptions,
    WorkflowEventActivationOptions, WorkflowStepScope,
};
use serde_json::Value;

#[test]
fn define_workflow_requires_run_as() {
    let err = define_workflow(DefineWorkflowOptions {
        id: "demo".to_string(),
        run_as: String::new(),
        paused: false,
    })
    .unwrap_err();
    assert!(err.to_string().contains("run_as"));
}

#[test]
fn typed_workflow_builder_matches_extract_row_example() {
    let spec = define_workflow(DefineWorkflowOptions {
        id: "extractRow".to_string(),
        run_as: "service_account:deal-hub-extraction".to_string(),
        paused: false,
    })
    .unwrap()
    .on(gestalt::WorkflowActivationConfig::Event(event(
        "deal_hub.analyses.extract.requested".to_string(),
        Some(|| {
            std::collections::BTreeMap::from([(
                "analysisId".to_string(),
                gestalt::workflow_ref_signal("data.analysisId"),
            )])
        }),
        WorkflowEventActivationOptions::default(),
    )))
    .step(
        "extract",
        gestalt::WorkflowStepConfig {
            app: Some(gestalt::WorkflowStepAppConfig {
                name: "dealHub".to_string(),
                operation: "analyses.extractRowWorkflow".to_string(),
                input: Some(|_scope: WorkflowStepScope| {
                    std::collections::BTreeMap::from([(
                        "analysisId".to_string(),
                        WorkflowStepScope::input("analysisId"),
                    )])
                }),
                input_map: None,
                connection: String::new(),
                instance: String::new(),
                credential_mode: String::new(),
            }),
            ..Default::default()
        },
    )
    .to_spec();

    let cases = load_workflow_lowering_contract().expect("load lowering contract");
    let expected = cases
        .iter()
        .find(|case| case.name == "extract_row")
        .expect("extract_row fixture")
        .expected_spec
        .clone();
    assert_eq!(canonical_workflow_definition_spec(&spec), expected);
}

#[test]
fn golden_fixtures_match_lowering_contract() {
    let cases = load_workflow_lowering_contract().expect("load lowering contract");
    for case in cases {
        let spec = build_workflow_from_lowering_case(&case)
            .expect("build workflow from lowering case")
            .to_spec();
        assert_eq!(
            canonical_workflow_definition_spec(&spec),
            case.expected_spec,
            "fixture {}",
            case.name
        );
    }
}

#[test]
fn resolve_workflow_definition_spec_accepts_builders_and_specs() {
    let builder = define_workflow(DefineWorkflowOptions {
        id: "extractRow".to_string(),
        run_as: "service_account:deal-hub-extraction".to_string(),
        paused: false,
    })
    .unwrap()
    .on(gestalt::WorkflowActivationConfig::Schedule(schedule(
        "0 2 * * *".to_string(),
        Some(|| {
            std::collections::BTreeMap::from([(
                "reason".to_string(),
                gestalt::workflow_ref_literal(Value::String("nightly".to_string())).unwrap(),
            )])
        }),
        Default::default(),
    )));

    let from_builder = resolve_workflow_definition_spec_from_builder(builder.clone());
    let from_spec = resolve_workflow_definition_spec(from_builder.clone());
    assert_eq!(from_builder, from_spec);
}
