use std::collections::BTreeMap;

use gestalt_plugin_sdk::proto::v1::{
    AuthenticatedUser, ConnectionMode, GetSessionCatalogRequest, PluginKind, PluginMetadata,
    ProviderMetadata, StoredUser, auth_plugin_client::AuthPluginClient,
    datastore_plugin_client::DatastorePluginClient, plugin_runtime_client::PluginRuntimeClient,
    provider_plugin_client::ProviderPluginClient,
};

#[test]
fn generated_messages_cover_each_proto_surface() {
    let provider = ProviderMetadata {
        name: "demo".to_owned(),
        connection_mode: 2,
        ..ProviderMetadata::default()
    };
    assert_eq!(
        ConnectionMode::try_from(provider.connection_mode)
            .expect("valid connection mode")
            .as_str_name(),
        "CONNECTION_MODE_USER"
    );

    let runtime = PluginMetadata {
        name: "demo".to_owned(),
        kind: 1,
        ..PluginMetadata::default()
    };
    assert_eq!(
        PluginKind::try_from(runtime.kind)
            .expect("valid plugin kind")
            .as_str_name(),
        "PLUGIN_KIND_INTEGRATION"
    );

    let auth = AuthenticatedUser {
        subject: "sub_123".to_owned(),
        email: "sdk@example.com".to_owned(),
        ..AuthenticatedUser::default()
    };
    assert_eq!(auth.email, "sdk@example.com");

    let datastore = StoredUser {
        id: "usr_123".to_owned(),
        email: "sdk@example.com".to_owned(),
        ..StoredUser::default()
    };
    assert_eq!(datastore.id, "usr_123");
}

#[test]
fn generated_grpc_clients_are_available() {
    let _ = std::any::type_name::<ProviderPluginClient<tonic::transport::Channel>>();
    let _ = std::any::type_name::<PluginRuntimeClient<tonic::transport::Channel>>();
    let _ = std::any::type_name::<AuthPluginClient<tonic::transport::Channel>>();
    let _ = std::any::type_name::<DatastorePluginClient<tonic::transport::Channel>>();
}

#[test]
fn generated_map_fields_use_btree_map() {
    let request = GetSessionCatalogRequest::default();

    assert_eq!(
        std::any::type_name_of_val(&request.connection_params),
        std::any::type_name::<BTreeMap<String, String>>(),
    );
}
