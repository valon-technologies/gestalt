use std::any::Any;
use std::collections::BTreeMap;
use std::future::Future;
use std::marker::PhantomData;
use std::pin::Pin;
use std::sync::Arc;

use schemars::JsonSchema;
use serde::Serialize;
use serde::de::DeserializeOwned;
use serde_json::Value;

use crate::api::{IntoResponse, Request};
use crate::catalog::{
    Catalog, CatalogOperation, OperationAnnotations, schema_json, schema_parameters,
};
use crate::error::{Error, Result};
use crate::provider_server::OperationResult;

#[derive(Clone, Debug)]
pub struct Operation<In, Out> {
    pub id: String,
    pub method: String,
    pub title: String,
    pub description: String,
    pub tags: Vec<String>,
    pub read_only: bool,
    pub visible: Option<bool>,
    _types: PhantomData<(In, Out)>,
}

impl<In, Out> Operation<In, Out>
where
    In: JsonSchema,
    Out: JsonSchema,
{
    pub fn new(id: impl Into<String>) -> Self {
        Self {
            id: id.into(),
            method: "POST".to_owned(),
            title: String::new(),
            description: String::new(),
            tags: Vec::new(),
            read_only: false,
            visible: None,
            _types: PhantomData,
        }
    }

    pub fn method(mut self, method: impl AsRef<str>) -> Self {
        let method = method.as_ref().trim().to_ascii_uppercase();
        if !method.is_empty() {
            self.method = method;
        }
        self
    }

    pub fn title(mut self, title: impl Into<String>) -> Self {
        self.title = title.into();
        self
    }

    pub fn description(mut self, description: impl Into<String>) -> Self {
        self.description = description.into();
        self
    }

    pub fn tags(mut self, tags: impl Into<Vec<String>>) -> Self {
        self.tags = tags.into();
        self
    }

    pub fn read_only(mut self, read_only: bool) -> Self {
        self.read_only = read_only;
        self
    }

    pub fn visible(mut self, visible: bool) -> Self {
        self.visible = Some(visible);
        self
    }
}

type Handler<P> = Arc<
    dyn Fn(Arc<P>, Value, Request) -> Pin<Box<dyn Future<Output = OperationResult> + Send>>
        + Send
        + Sync,
>;

pub struct Router<P> {
    catalog: Catalog,
    handlers: BTreeMap<String, Handler<P>>,
}

impl<P> Clone for Router<P> {
    fn clone(&self) -> Self {
        Self {
            catalog: self.catalog.clone(),
            handlers: self.handlers.clone(),
        }
    }
}

impl<P> Default for Router<P> {
    fn default() -> Self {
        Self::new()
    }
}

impl<P> Router<P> {
    pub fn new() -> Self {
        Self {
            catalog: Catalog::default(),
            handlers: BTreeMap::new(),
        }
    }

    pub fn with_name(mut self, name: impl Into<String>) -> Self {
        let name = name.into();
        if !name.trim().is_empty() {
            self.catalog.name = name;
        }
        self
    }

    pub fn catalog(&self) -> Catalog {
        self.catalog.clone()
    }

    pub async fn execute(
        &self,
        provider: Arc<P>,
        operation: &str,
        params: Value,
        request: Request,
    ) -> OperationResult {
        let Some(handler) = self.handlers.get(operation) else {
            return OperationResult::error(
                http::StatusCode::NOT_FOUND.as_u16(),
                "unknown operation",
            );
        };

        match tokio::spawn(handler(provider, params, request)).await {
            Ok(result) => result,
            Err(error) => OperationResult::error(
                http::StatusCode::INTERNAL_SERVER_ERROR.as_u16(),
                join_error_message(error),
            ),
        }
    }
}

impl<P> Router<P>
where
    P: Send + Sync + 'static,
{
    pub fn register<In, Out, F, Fut, R, E>(
        mut self,
        operation: Operation<In, Out>,
        handler: F,
    ) -> Result<Self>
    where
        In: DeserializeOwned + JsonSchema + Send + 'static,
        Out: Serialize + JsonSchema + Send + 'static,
        F: Fn(Arc<P>, In, Request) -> Fut + Send + Sync + 'static,
        Fut: Future<Output = std::result::Result<R, E>> + Send + 'static,
        R: IntoResponse<Out> + Send + 'static,
        E: Into<Error> + Send + 'static,
    {
        let operation_id = operation.id.trim();
        if operation_id.is_empty() {
            return Err(Error::bad_request("operation id is required"));
        }
        if self.handlers.contains_key(operation_id) {
            return Err(Error::bad_request(format!(
                "duplicate operation id {:?}",
                operation_id
            )));
        }

        let input_schema = schema_json::<In>()?;
        let output_schema = schema_json::<Out>()?;
        self.catalog.operations.push(CatalogOperation {
            id: operation_id.to_owned(),
            method: operation.method.clone(),
            title: operation.title.trim().to_owned(),
            description: operation.description.trim().to_owned(),
            input_schema: Some(input_schema.clone()),
            output_schema: Some(output_schema),
            annotations: Some(OperationAnnotations {
                read_only_hint: operation.read_only.then_some(true),
                ..OperationAnnotations::default()
            }),
            parameters: schema_parameters(&input_schema),
            required_scopes: Vec::new(),
            tags: operation.tags.clone(),
            read_only: operation.read_only,
            visible: operation.visible,
        });

        let handler = Arc::new(handler);
        let operation_id = operation_id.to_owned();
        self.handlers.insert(
            operation_id.clone(),
            Arc::new(
                move |provider: Arc<P>, raw_params: Value, request: Request| {
                    let handler = Arc::clone(&handler);
                    let operation_id = operation_id.clone();
                    Box::pin(async move {
                        let input = match decode_params::<In>(&operation_id, raw_params) {
                            Ok(input) => input,
                            Err(error) => return OperationResult::from_error(error),
                        };

                        match handler(provider, input, request).await {
                            Ok(response) => {
                                OperationResult::from_response(response.into_response())
                            }
                            Err(error) => OperationResult::from_error(error.into()),
                        }
                    })
                },
            ),
        );

        Ok(self)
    }
}

fn decode_params<In: DeserializeOwned>(operation_id: &str, raw_params: Value) -> Result<In> {
    match serde_json::from_value::<In>(raw_params.clone()) {
        Ok(input) => Ok(input),
        Err(error) if is_empty_object(&raw_params) => serde_json::from_value::<In>(Value::Null)
            .map_err(|_| {
                Error::bad_request(format!("decode params for {:?}: {}", operation_id, error))
            }),
        Err(error) => Err(Error::bad_request(format!(
            "decode params for {:?}: {}",
            operation_id, error
        ))),
    }
}

fn is_empty_object(value: &Value) -> bool {
    matches!(value, Value::Object(map) if map.is_empty())
}

fn join_error_message(error: tokio::task::JoinError) -> String {
    if error.is_panic() {
        return panic_message(error.try_into_panic().expect("panic payload"));
    }
    error.to_string()
}

fn panic_message(payload: Box<dyn Any + Send + 'static>) -> String {
    if let Some(text) = payload.downcast_ref::<&'static str>() {
        (*text).to_owned()
    } else if let Some(text) = payload.downcast_ref::<String>() {
        text.clone()
    } else {
        "panic in Gestalt operation".to_owned()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[derive(Clone, Default)]
    struct TestProvider;

    #[derive(serde::Deserialize, schemars::JsonSchema)]
    struct Input {
        query: String,
    }

    #[derive(serde::Serialize, schemars::JsonSchema)]
    struct Output {
        query: String,
    }

    #[tokio::test]
    async fn router_execute_returns_not_found_for_unknown_operation() {
        let router = Router::<TestProvider>::new();
        let result = router
            .execute(
                Arc::new(TestProvider),
                "missing",
                Value::Object(Default::default()),
                Request::default(),
            )
            .await;
        assert_eq!(result.status, http::StatusCode::NOT_FOUND.as_u16());
    }

    #[test]
    fn router_rejects_duplicate_ids() {
        let router = Router::<TestProvider>::new()
            .register(
                Operation::<Input, Output>::new("search"),
                |_provider, input, _request| async move {
                    Ok::<crate::Response<Output>, std::convert::Infallible>(crate::ok(Output {
                        query: input.query,
                    }))
                },
            )
            .expect("first registration");
        let result = router.register(
            Operation::<Input, Output>::new("search"),
            |_provider, input, _request| async move {
                Ok::<crate::Response<Output>, std::convert::Infallible>(crate::ok(Output {
                    query: input.query,
                }))
            },
        );

        match result {
            Ok(_) => panic!("duplicate id should fail"),
            Err(err) => assert!(err.message().contains("duplicate operation id")),
        }
    }
}
