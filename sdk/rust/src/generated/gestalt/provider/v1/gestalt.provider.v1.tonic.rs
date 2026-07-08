// @generated
/// Generated client implementations.
pub mod app_provider_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    /** AppProvider models the shared Gestalt integration-provider protocol.
    */
    #[derive(Debug, Clone)]
    pub struct AppProviderClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl AppProviderClient<tonic::transport::Channel> {
        /// Attempt to create a new client by connecting to a given endpoint.
        pub async fn connect<D>(dst: D) -> Result<Self, tonic::transport::Error>
        where
            D: TryInto<tonic::transport::Endpoint>,
            D::Error: Into<StdError>,
        {
            let conn = tonic::transport::Endpoint::new(dst)?.connect().await?;
            Ok(Self::new(conn))
        }
    }
    impl<T> AppProviderClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::Body>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + std::marker::Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + std::marker::Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> AppProviderClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                    http::Request<tonic::body::Body>,
                    Response = http::Response<
                        <T as tonic::client::GrpcService<tonic::body::Body>>::ResponseBody,
                    >,
                >,
            <T as tonic::codegen::Service<http::Request<tonic::body::Body>>>::Error:
                Into<StdError> + std::marker::Send + std::marker::Sync,
        {
            AppProviderClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        ///
        pub async fn get_metadata(
            &mut self,
            request: impl tonic::IntoRequest<()>,
        ) -> std::result::Result<tonic::Response<super::ProviderMetadata>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AppProvider/GetMetadata",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AppProvider",
                "GetMetadata",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn start_provider(
            &mut self,
            request: impl tonic::IntoRequest<super::StartProviderRequest>,
        ) -> std::result::Result<tonic::Response<super::StartProviderResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AppProvider/StartProvider",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AppProvider",
                "StartProvider",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn execute(
            &mut self,
            request: impl tonic::IntoRequest<super::ExecuteRequest>,
        ) -> std::result::Result<tonic::Response<super::OperationResult>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.AppProvider/Execute");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AppProvider",
                "Execute",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn resolve_http_subject(
            &mut self,
            request: impl tonic::IntoRequest<super::ResolveHttpSubjectRequest>,
        ) -> std::result::Result<tonic::Response<super::ResolveHttpSubjectResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AppProvider/ResolveHTTPSubject",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AppProvider",
                "ResolveHTTPSubject",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_session_catalog(
            &mut self,
            request: impl tonic::IntoRequest<super::GetSessionCatalogRequest>,
        ) -> std::result::Result<tonic::Response<super::GetSessionCatalogResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AppProvider/GetSessionCatalog",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AppProvider",
                "GetSessionCatalog",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod app_provider_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with AppProviderServer.
    #[async_trait]
    pub trait AppProvider: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn get_metadata(
            &self,
            request: tonic::Request<()>,
        ) -> std::result::Result<tonic::Response<super::ProviderMetadata>, tonic::Status>;
        ///
        async fn start_provider(
            &self,
            request: tonic::Request<super::StartProviderRequest>,
        ) -> std::result::Result<tonic::Response<super::StartProviderResponse>, tonic::Status>;
        ///
        async fn execute(
            &self,
            request: tonic::Request<super::ExecuteRequest>,
        ) -> std::result::Result<tonic::Response<super::OperationResult>, tonic::Status>;
        ///
        async fn resolve_http_subject(
            &self,
            request: tonic::Request<super::ResolveHttpSubjectRequest>,
        ) -> std::result::Result<tonic::Response<super::ResolveHttpSubjectResponse>, tonic::Status>;
        ///
        async fn get_session_catalog(
            &self,
            request: tonic::Request<super::GetSessionCatalogRequest>,
        ) -> std::result::Result<tonic::Response<super::GetSessionCatalogResponse>, tonic::Status>;
    }
    /** AppProvider models the shared Gestalt integration-provider protocol.
    */
    #[derive(Debug)]
    pub struct AppProviderServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> AppProviderServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for AppProviderServer<T>
    where
        T: AppProvider,
        B: Body + std::marker::Send + 'static,
        B::Error: Into<StdError> + std::marker::Send + 'static,
    {
        type Response = http::Response<tonic::body::Body>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            match req.uri().path() {
                "/gestalt.provider.v1.AppProvider/GetMetadata" => {
                    #[allow(non_camel_case_types)]
                    struct GetMetadataSvc<T: AppProvider>(pub Arc<T>);
                    impl<T: AppProvider> tonic::server::UnaryService<()> for GetMetadataSvc<T> {
                        type Response = super::ProviderMetadata;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(&mut self, request: tonic::Request<()>) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AppProvider>::get_metadata(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetMetadataSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.AppProvider/StartProvider" => {
                    #[allow(non_camel_case_types)]
                    struct StartProviderSvc<T: AppProvider>(pub Arc<T>);
                    impl<T: AppProvider> tonic::server::UnaryService<super::StartProviderRequest>
                        for StartProviderSvc<T>
                    {
                        type Response = super::StartProviderResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::StartProviderRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AppProvider>::start_provider(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = StartProviderSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.AppProvider/Execute" => {
                    #[allow(non_camel_case_types)]
                    struct ExecuteSvc<T: AppProvider>(pub Arc<T>);
                    impl<T: AppProvider> tonic::server::UnaryService<super::ExecuteRequest> for ExecuteSvc<T> {
                        type Response = super::OperationResult;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ExecuteRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as AppProvider>::execute(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ExecuteSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.AppProvider/ResolveHTTPSubject" => {
                    #[allow(non_camel_case_types)]
                    struct ResolveHTTPSubjectSvc<T: AppProvider>(pub Arc<T>);
                    impl<T: AppProvider>
                        tonic::server::UnaryService<super::ResolveHttpSubjectRequest>
                        for ResolveHTTPSubjectSvc<T>
                    {
                        type Response = super::ResolveHttpSubjectResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ResolveHttpSubjectRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AppProvider>::resolve_http_subject(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ResolveHTTPSubjectSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.AppProvider/GetSessionCatalog" => {
                    #[allow(non_camel_case_types)]
                    struct GetSessionCatalogSvc<T: AppProvider>(pub Arc<T>);
                    impl<T: AppProvider>
                        tonic::server::UnaryService<super::GetSessionCatalogRequest>
                        for GetSessionCatalogSvc<T>
                    {
                        type Response = super::GetSessionCatalogResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetSessionCatalogRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AppProvider>::get_session_catalog(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetSessionCatalogSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => Box::pin(async move {
                    let mut response = http::Response::new(tonic::body::Body::default());
                    let headers = response.headers_mut();
                    headers.insert(
                        tonic::Status::GRPC_STATUS,
                        (tonic::Code::Unimplemented as i32).into(),
                    );
                    headers.insert(
                        http::header::CONTENT_TYPE,
                        tonic::metadata::GRPC_CONTENT_TYPE,
                    );
                    Ok(response)
                }),
            }
        }
    }
    impl<T> Clone for AppProviderServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    /// Generated gRPC service name
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.AppProvider";
    impl<T> tonic::server::NamedService for AppProviderServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod app_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    ///
    #[derive(Debug, Clone)]
    pub struct AppClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl AppClient<tonic::transport::Channel> {
        /// Attempt to create a new client by connecting to a given endpoint.
        pub async fn connect<D>(dst: D) -> Result<Self, tonic::transport::Error>
        where
            D: TryInto<tonic::transport::Endpoint>,
            D::Error: Into<StdError>,
        {
            let conn = tonic::transport::Endpoint::new(dst)?.connect().await?;
            Ok(Self::new(conn))
        }
    }
    impl<T> AppClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::Body>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + std::marker::Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + std::marker::Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> AppClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                    http::Request<tonic::body::Body>,
                    Response = http::Response<
                        <T as tonic::client::GrpcService<tonic::body::Body>>::ResponseBody,
                    >,
                >,
            <T as tonic::codegen::Service<http::Request<tonic::body::Body>>>::Error:
                Into<StdError> + std::marker::Send + std::marker::Sync,
        {
            AppClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        pub async fn invoke(
            &mut self,
            request: impl tonic::IntoRequest<super::AppInvokeRequest>,
        ) -> std::result::Result<tonic::Response<super::OperationResult>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.App/Invoke");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.App", "Invoke"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn invoke_graph_ql(
            &mut self,
            request: impl tonic::IntoRequest<super::AppInvokeGraphQlRequest>,
        ) -> std::result::Result<tonic::Response<super::OperationResult>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.App/InvokeGraphQL");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.App", "InvokeGraphQL"));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod app_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with AppServer.
    #[async_trait]
    pub trait App: std::marker::Send + std::marker::Sync + 'static {
        async fn invoke(
            &self,
            request: tonic::Request<super::AppInvokeRequest>,
        ) -> std::result::Result<tonic::Response<super::OperationResult>, tonic::Status>;
        ///
        async fn invoke_graph_ql(
            &self,
            request: tonic::Request<super::AppInvokeGraphQlRequest>,
        ) -> std::result::Result<tonic::Response<super::OperationResult>, tonic::Status>;
    }
    ///
    #[derive(Debug)]
    pub struct AppServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> AppServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for AppServer<T>
    where
        T: App,
        B: Body + std::marker::Send + 'static,
        B::Error: Into<StdError> + std::marker::Send + 'static,
    {
        type Response = http::Response<tonic::body::Body>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            match req.uri().path() {
                "/gestalt.provider.v1.App/Invoke" => {
                    #[allow(non_camel_case_types)]
                    struct InvokeSvc<T: App>(pub Arc<T>);
                    impl<T: App> tonic::server::UnaryService<super::AppInvokeRequest> for InvokeSvc<T> {
                        type Response = super::OperationResult;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::AppInvokeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as App>::invoke(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = InvokeSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.App/InvokeGraphQL" => {
                    #[allow(non_camel_case_types)]
                    struct InvokeGraphQLSvc<T: App>(pub Arc<T>);
                    impl<T: App> tonic::server::UnaryService<super::AppInvokeGraphQlRequest> for InvokeGraphQLSvc<T> {
                        type Response = super::OperationResult;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::AppInvokeGraphQlRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as App>::invoke_graph_ql(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = InvokeGraphQLSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => Box::pin(async move {
                    let mut response = http::Response::new(tonic::body::Body::default());
                    let headers = response.headers_mut();
                    headers.insert(
                        tonic::Status::GRPC_STATUS,
                        (tonic::Code::Unimplemented as i32).into(),
                    );
                    headers.insert(
                        http::header::CONTENT_TYPE,
                        tonic::metadata::GRPC_CONTENT_TYPE,
                    );
                    Ok(response)
                }),
            }
        }
    }
    impl<T> Clone for AppServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    /// Generated gRPC service name
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.App";
    impl<T> tonic::server::NamedService for AppServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod agent_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    /** Agent is the authoritative agent data boundary. Read RPCs for
     sessions, turns, turn events, and interactions should use provider-owned
     control-plane state and should not require a live execution sandbox,
     pod-level transport, or cached tunnel.
    */
    #[derive(Debug, Clone)]
    pub struct AgentClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl AgentClient<tonic::transport::Channel> {
        /// Attempt to create a new client by connecting to a given endpoint.
        pub async fn connect<D>(dst: D) -> Result<Self, tonic::transport::Error>
        where
            D: TryInto<tonic::transport::Endpoint>,
            D::Error: Into<StdError>,
        {
            let conn = tonic::transport::Endpoint::new(dst)?.connect().await?;
            Ok(Self::new(conn))
        }
    }
    impl<T> AgentClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::Body>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + std::marker::Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + std::marker::Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> AgentClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                    http::Request<tonic::body::Body>,
                    Response = http::Response<
                        <T as tonic::client::GrpcService<tonic::body::Body>>::ResponseBody,
                    >,
                >,
            <T as tonic::codegen::Service<http::Request<tonic::body::Body>>>::Error:
                Into<StdError> + std::marker::Send + std::marker::Sync,
        {
            AgentClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        pub async fn create_session(
            &mut self,
            request: impl tonic::IntoRequest<super::CreateAgentProviderSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentSession>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Agent/CreateSession");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Agent",
                "CreateSession",
            ));
            self.inner.unary(req, path, codec).await
        }
        pub async fn get_session(
            &mut self,
            request: impl tonic::IntoRequest<super::GetAgentProviderSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentSession>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Agent/GetSession");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Agent", "GetSession"));
            self.inner.unary(req, path, codec).await
        }
        pub async fn list_sessions(
            &mut self,
            request: impl tonic::IntoRequest<super::ListAgentProviderSessionsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListAgentProviderSessionsResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Agent/ListSessions");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Agent", "ListSessions"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn update_session(
            &mut self,
            request: impl tonic::IntoRequest<super::UpdateAgentProviderSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentSession>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Agent/UpdateSession");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Agent",
                "UpdateSession",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn create_turn(
            &mut self,
            request: impl tonic::IntoRequest<super::CreateAgentProviderTurnRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentTurn>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Agent/CreateTurn");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Agent", "CreateTurn"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_turn(
            &mut self,
            request: impl tonic::IntoRequest<super::GetAgentProviderTurnRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentTurn>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Agent/GetTurn");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Agent", "GetTurn"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_turns(
            &mut self,
            request: impl tonic::IntoRequest<super::ListAgentProviderTurnsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListAgentProviderTurnsResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Agent/ListTurns");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Agent", "ListTurns"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn cancel_turn(
            &mut self,
            request: impl tonic::IntoRequest<super::CancelAgentProviderTurnRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentTurn>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Agent/CancelTurn");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Agent", "CancelTurn"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_turn_events(
            &mut self,
            request: impl tonic::IntoRequest<super::ListAgentProviderTurnEventsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListAgentProviderTurnEventsResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Agent/ListTurnEvents");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Agent",
                "ListTurnEvents",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_interaction(
            &mut self,
            request: impl tonic::IntoRequest<super::GetAgentProviderInteractionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentInteraction>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Agent/GetInteraction");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Agent",
                "GetInteraction",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_interactions(
            &mut self,
            request: impl tonic::IntoRequest<super::ListAgentProviderInteractionsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListAgentProviderInteractionsResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Agent/ListInteractions");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Agent",
                "ListInteractions",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn resolve_interaction(
            &mut self,
            request: impl tonic::IntoRequest<super::ResolveAgentProviderInteractionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentInteraction>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Agent/ResolveInteraction",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Agent",
                "ResolveInteraction",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_capabilities(
            &mut self,
            request: impl tonic::IntoRequest<super::GetAgentProviderCapabilitiesRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentProviderCapabilities>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Agent/GetCapabilities");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Agent",
                "GetCapabilities",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod agent_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with AgentServer.
    #[async_trait]
    pub trait Agent: std::marker::Send + std::marker::Sync + 'static {
        async fn create_session(
            &self,
            request: tonic::Request<super::CreateAgentProviderSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentSession>, tonic::Status>;
        async fn get_session(
            &self,
            request: tonic::Request<super::GetAgentProviderSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentSession>, tonic::Status>;
        async fn list_sessions(
            &self,
            request: tonic::Request<super::ListAgentProviderSessionsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListAgentProviderSessionsResponse>,
            tonic::Status,
        >;
        ///
        async fn update_session(
            &self,
            request: tonic::Request<super::UpdateAgentProviderSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentSession>, tonic::Status>;
        ///
        async fn create_turn(
            &self,
            request: tonic::Request<super::CreateAgentProviderTurnRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentTurn>, tonic::Status>;
        ///
        async fn get_turn(
            &self,
            request: tonic::Request<super::GetAgentProviderTurnRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentTurn>, tonic::Status>;
        ///
        async fn list_turns(
            &self,
            request: tonic::Request<super::ListAgentProviderTurnsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListAgentProviderTurnsResponse>,
            tonic::Status,
        >;
        ///
        async fn cancel_turn(
            &self,
            request: tonic::Request<super::CancelAgentProviderTurnRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentTurn>, tonic::Status>;
        ///
        async fn list_turn_events(
            &self,
            request: tonic::Request<super::ListAgentProviderTurnEventsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListAgentProviderTurnEventsResponse>,
            tonic::Status,
        >;
        ///
        async fn get_interaction(
            &self,
            request: tonic::Request<super::GetAgentProviderInteractionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentInteraction>, tonic::Status>;
        ///
        async fn list_interactions(
            &self,
            request: tonic::Request<super::ListAgentProviderInteractionsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListAgentProviderInteractionsResponse>,
            tonic::Status,
        >;
        ///
        async fn resolve_interaction(
            &self,
            request: tonic::Request<super::ResolveAgentProviderInteractionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentInteraction>, tonic::Status>;
        ///
        async fn get_capabilities(
            &self,
            request: tonic::Request<super::GetAgentProviderCapabilitiesRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentProviderCapabilities>, tonic::Status>;
    }
    /** Agent is the authoritative agent data boundary. Read RPCs for
     sessions, turns, turn events, and interactions should use provider-owned
     control-plane state and should not require a live execution sandbox,
     pod-level transport, or cached tunnel.
    */
    #[derive(Debug)]
    pub struct AgentServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> AgentServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for AgentServer<T>
    where
        T: Agent,
        B: Body + std::marker::Send + 'static,
        B::Error: Into<StdError> + std::marker::Send + 'static,
    {
        type Response = http::Response<tonic::body::Body>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            match req.uri().path() {
                "/gestalt.provider.v1.Agent/CreateSession" => {
                    #[allow(non_camel_case_types)]
                    struct CreateSessionSvc<T: Agent>(pub Arc<T>);
                    impl<T: Agent>
                        tonic::server::UnaryService<super::CreateAgentProviderSessionRequest>
                        for CreateSessionSvc<T>
                    {
                        type Response = super::AgentSession;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CreateAgentProviderSessionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Agent>::create_session(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = CreateSessionSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Agent/GetSession" => {
                    #[allow(non_camel_case_types)]
                    struct GetSessionSvc<T: Agent>(pub Arc<T>);
                    impl<T: Agent>
                        tonic::server::UnaryService<super::GetAgentProviderSessionRequest>
                        for GetSessionSvc<T>
                    {
                        type Response = super::AgentSession;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetAgentProviderSessionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Agent>::get_session(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetSessionSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Agent/ListSessions" => {
                    #[allow(non_camel_case_types)]
                    struct ListSessionsSvc<T: Agent>(pub Arc<T>);
                    impl<T: Agent>
                        tonic::server::UnaryService<super::ListAgentProviderSessionsRequest>
                        for ListSessionsSvc<T>
                    {
                        type Response = super::ListAgentProviderSessionsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListAgentProviderSessionsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Agent>::list_sessions(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ListSessionsSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Agent/UpdateSession" => {
                    #[allow(non_camel_case_types)]
                    struct UpdateSessionSvc<T: Agent>(pub Arc<T>);
                    impl<T: Agent>
                        tonic::server::UnaryService<super::UpdateAgentProviderSessionRequest>
                        for UpdateSessionSvc<T>
                    {
                        type Response = super::AgentSession;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::UpdateAgentProviderSessionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Agent>::update_session(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = UpdateSessionSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Agent/CreateTurn" => {
                    #[allow(non_camel_case_types)]
                    struct CreateTurnSvc<T: Agent>(pub Arc<T>);
                    impl<T: Agent>
                        tonic::server::UnaryService<super::CreateAgentProviderTurnRequest>
                        for CreateTurnSvc<T>
                    {
                        type Response = super::AgentTurn;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CreateAgentProviderTurnRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Agent>::create_turn(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = CreateTurnSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Agent/GetTurn" => {
                    #[allow(non_camel_case_types)]
                    struct GetTurnSvc<T: Agent>(pub Arc<T>);
                    impl<T: Agent> tonic::server::UnaryService<super::GetAgentProviderTurnRequest> for GetTurnSvc<T> {
                        type Response = super::AgentTurn;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetAgentProviderTurnRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as Agent>::get_turn(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetTurnSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Agent/ListTurns" => {
                    #[allow(non_camel_case_types)]
                    struct ListTurnsSvc<T: Agent>(pub Arc<T>);
                    impl<T: Agent> tonic::server::UnaryService<super::ListAgentProviderTurnsRequest>
                        for ListTurnsSvc<T>
                    {
                        type Response = super::ListAgentProviderTurnsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListAgentProviderTurnsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Agent>::list_turns(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ListTurnsSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Agent/CancelTurn" => {
                    #[allow(non_camel_case_types)]
                    struct CancelTurnSvc<T: Agent>(pub Arc<T>);
                    impl<T: Agent>
                        tonic::server::UnaryService<super::CancelAgentProviderTurnRequest>
                        for CancelTurnSvc<T>
                    {
                        type Response = super::AgentTurn;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CancelAgentProviderTurnRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Agent>::cancel_turn(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = CancelTurnSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Agent/ListTurnEvents" => {
                    #[allow(non_camel_case_types)]
                    struct ListTurnEventsSvc<T: Agent>(pub Arc<T>);
                    impl<T: Agent>
                        tonic::server::UnaryService<super::ListAgentProviderTurnEventsRequest>
                        for ListTurnEventsSvc<T>
                    {
                        type Response = super::ListAgentProviderTurnEventsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListAgentProviderTurnEventsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Agent>::list_turn_events(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ListTurnEventsSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Agent/GetInteraction" => {
                    #[allow(non_camel_case_types)]
                    struct GetInteractionSvc<T: Agent>(pub Arc<T>);
                    impl<T: Agent>
                        tonic::server::UnaryService<super::GetAgentProviderInteractionRequest>
                        for GetInteractionSvc<T>
                    {
                        type Response = super::AgentInteraction;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetAgentProviderInteractionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Agent>::get_interaction(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetInteractionSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Agent/ListInteractions" => {
                    #[allow(non_camel_case_types)]
                    struct ListInteractionsSvc<T: Agent>(pub Arc<T>);
                    impl<T: Agent>
                        tonic::server::UnaryService<super::ListAgentProviderInteractionsRequest>
                        for ListInteractionsSvc<T>
                    {
                        type Response = super::ListAgentProviderInteractionsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListAgentProviderInteractionsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Agent>::list_interactions(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ListInteractionsSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Agent/ResolveInteraction" => {
                    #[allow(non_camel_case_types)]
                    struct ResolveInteractionSvc<T: Agent>(pub Arc<T>);
                    impl<T: Agent>
                        tonic::server::UnaryService<super::ResolveAgentProviderInteractionRequest>
                        for ResolveInteractionSvc<T>
                    {
                        type Response = super::AgentInteraction;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ResolveAgentProviderInteractionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Agent>::resolve_interaction(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ResolveInteractionSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Agent/GetCapabilities" => {
                    #[allow(non_camel_case_types)]
                    struct GetCapabilitiesSvc<T: Agent>(pub Arc<T>);
                    impl<T: Agent>
                        tonic::server::UnaryService<super::GetAgentProviderCapabilitiesRequest>
                        for GetCapabilitiesSvc<T>
                    {
                        type Response = super::AgentProviderCapabilities;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetAgentProviderCapabilitiesRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Agent>::get_capabilities(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetCapabilitiesSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => Box::pin(async move {
                    let mut response = http::Response::new(tonic::body::Body::default());
                    let headers = response.headers_mut();
                    headers.insert(
                        tonic::Status::GRPC_STATUS,
                        (tonic::Code::Unimplemented as i32).into(),
                    );
                    headers.insert(
                        http::header::CONTENT_TYPE,
                        tonic::metadata::GRPC_CONTENT_TYPE,
                    );
                    Ok(response)
                }),
            }
        }
    }
    impl<T> Clone for AgentServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    /// Generated gRPC service name
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.Agent";
    impl<T> tonic::server::NamedService for AgentServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod authorization_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    ///
    #[derive(Debug, Clone)]
    pub struct AuthorizationClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl AuthorizationClient<tonic::transport::Channel> {
        /// Attempt to create a new client by connecting to a given endpoint.
        pub async fn connect<D>(dst: D) -> Result<Self, tonic::transport::Error>
        where
            D: TryInto<tonic::transport::Endpoint>,
            D::Error: Into<StdError>,
        {
            let conn = tonic::transport::Endpoint::new(dst)?.connect().await?;
            Ok(Self::new(conn))
        }
    }
    impl<T> AuthorizationClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::Body>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + std::marker::Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + std::marker::Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> AuthorizationClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                    http::Request<tonic::body::Body>,
                    Response = http::Response<
                        <T as tonic::client::GrpcService<tonic::body::Body>>::ResponseBody,
                    >,
                >,
            <T as tonic::codegen::Service<http::Request<tonic::body::Body>>>::Error:
                Into<StdError> + std::marker::Send + std::marker::Sync,
        {
            AuthorizationClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        pub async fn check_access(
            &mut self,
            request: impl tonic::IntoRequest<super::CheckAccessRequest>,
        ) -> std::result::Result<tonic::Response<super::CheckAccessResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Authorization/CheckAccess",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Authorization",
                "CheckAccess",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn check_access_many(
            &mut self,
            request: impl tonic::IntoRequest<super::CheckAccessManyRequest>,
        ) -> std::result::Result<tonic::Response<super::CheckAccessManyResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Authorization/CheckAccessMany",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Authorization",
                "CheckAccessMany",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_relationships(
            &mut self,
            request: impl tonic::IntoRequest<super::ListRelationshipsRequest>,
        ) -> std::result::Result<tonic::Response<super::ListRelationshipsResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Authorization/ListRelationships",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Authorization",
                "ListRelationships",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn add_relationship(
            &mut self,
            request: impl tonic::IntoRequest<super::AddRelationshipRequest>,
        ) -> std::result::Result<tonic::Response<super::AddRelationshipResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Authorization/AddRelationship",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Authorization",
                "AddRelationship",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn delete_relationship(
            &mut self,
            request: impl tonic::IntoRequest<super::DeleteRelationshipRequest>,
        ) -> std::result::Result<tonic::Response<super::DeleteRelationshipResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Authorization/DeleteRelationship",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Authorization",
                "DeleteRelationship",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn set_authorization_state(
            &mut self,
            request: impl tonic::IntoRequest<super::SetAuthorizationStateRequest>,
        ) -> std::result::Result<tonic::Response<super::SetAuthorizationStateResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Authorization/SetAuthorizationState",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Authorization",
                "SetAuthorizationState",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_active_model_ref(
            &mut self,
            request: impl tonic::IntoRequest<()>,
        ) -> std::result::Result<tonic::Response<super::GetActiveModelRefResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Authorization/GetActiveModelRef",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Authorization",
                "GetActiveModelRef",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn set_active_model(
            &mut self,
            request: impl tonic::IntoRequest<super::SetActiveModelRequest>,
        ) -> std::result::Result<tonic::Response<super::SetActiveModelResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Authorization/SetActiveModel",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Authorization",
                "SetActiveModel",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_active_model_resource_types(
            &mut self,
            request: impl tonic::IntoRequest<super::ListActiveModelResourceTypesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListActiveModelResourceTypesResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Authorization/ListActiveModelResourceTypes",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Authorization",
                "ListActiveModelResourceTypes",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod authorization_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with AuthorizationServer.
    #[async_trait]
    pub trait Authorization: std::marker::Send + std::marker::Sync + 'static {
        async fn check_access(
            &self,
            request: tonic::Request<super::CheckAccessRequest>,
        ) -> std::result::Result<tonic::Response<super::CheckAccessResponse>, tonic::Status>;
        ///
        async fn check_access_many(
            &self,
            request: tonic::Request<super::CheckAccessManyRequest>,
        ) -> std::result::Result<tonic::Response<super::CheckAccessManyResponse>, tonic::Status>;
        ///
        async fn list_relationships(
            &self,
            request: tonic::Request<super::ListRelationshipsRequest>,
        ) -> std::result::Result<tonic::Response<super::ListRelationshipsResponse>, tonic::Status>;
        ///
        async fn add_relationship(
            &self,
            request: tonic::Request<super::AddRelationshipRequest>,
        ) -> std::result::Result<tonic::Response<super::AddRelationshipResponse>, tonic::Status>;
        ///
        async fn delete_relationship(
            &self,
            request: tonic::Request<super::DeleteRelationshipRequest>,
        ) -> std::result::Result<tonic::Response<super::DeleteRelationshipResponse>, tonic::Status>;
        ///
        async fn set_authorization_state(
            &self,
            request: tonic::Request<super::SetAuthorizationStateRequest>,
        ) -> std::result::Result<tonic::Response<super::SetAuthorizationStateResponse>, tonic::Status>;
        ///
        async fn get_active_model_ref(
            &self,
            request: tonic::Request<()>,
        ) -> std::result::Result<tonic::Response<super::GetActiveModelRefResponse>, tonic::Status>;
        ///
        async fn set_active_model(
            &self,
            request: tonic::Request<super::SetActiveModelRequest>,
        ) -> std::result::Result<tonic::Response<super::SetActiveModelResponse>, tonic::Status>;
        ///
        async fn list_active_model_resource_types(
            &self,
            request: tonic::Request<super::ListActiveModelResourceTypesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListActiveModelResourceTypesResponse>,
            tonic::Status,
        >;
    }
    ///
    #[derive(Debug)]
    pub struct AuthorizationServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> AuthorizationServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for AuthorizationServer<T>
    where
        T: Authorization,
        B: Body + std::marker::Send + 'static,
        B::Error: Into<StdError> + std::marker::Send + 'static,
    {
        type Response = http::Response<tonic::body::Body>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            match req.uri().path() {
                "/gestalt.provider.v1.Authorization/CheckAccess" => {
                    #[allow(non_camel_case_types)]
                    struct CheckAccessSvc<T: Authorization>(pub Arc<T>);
                    impl<T: Authorization> tonic::server::UnaryService<super::CheckAccessRequest>
                        for CheckAccessSvc<T>
                    {
                        type Response = super::CheckAccessResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CheckAccessRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Authorization>::check_access(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = CheckAccessSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Authorization/CheckAccessMany" => {
                    #[allow(non_camel_case_types)]
                    struct CheckAccessManySvc<T: Authorization>(pub Arc<T>);
                    impl<T: Authorization>
                        tonic::server::UnaryService<super::CheckAccessManyRequest>
                        for CheckAccessManySvc<T>
                    {
                        type Response = super::CheckAccessManyResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CheckAccessManyRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Authorization>::check_access_many(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = CheckAccessManySvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Authorization/ListRelationships" => {
                    #[allow(non_camel_case_types)]
                    struct ListRelationshipsSvc<T: Authorization>(pub Arc<T>);
                    impl<T: Authorization>
                        tonic::server::UnaryService<super::ListRelationshipsRequest>
                        for ListRelationshipsSvc<T>
                    {
                        type Response = super::ListRelationshipsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListRelationshipsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Authorization>::list_relationships(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ListRelationshipsSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Authorization/AddRelationship" => {
                    #[allow(non_camel_case_types)]
                    struct AddRelationshipSvc<T: Authorization>(pub Arc<T>);
                    impl<T: Authorization>
                        tonic::server::UnaryService<super::AddRelationshipRequest>
                        for AddRelationshipSvc<T>
                    {
                        type Response = super::AddRelationshipResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::AddRelationshipRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Authorization>::add_relationship(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = AddRelationshipSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Authorization/DeleteRelationship" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteRelationshipSvc<T: Authorization>(pub Arc<T>);
                    impl<T: Authorization>
                        tonic::server::UnaryService<super::DeleteRelationshipRequest>
                        for DeleteRelationshipSvc<T>
                    {
                        type Response = super::DeleteRelationshipResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::DeleteRelationshipRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Authorization>::delete_relationship(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = DeleteRelationshipSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Authorization/SetAuthorizationState" => {
                    #[allow(non_camel_case_types)]
                    struct SetAuthorizationStateSvc<T: Authorization>(pub Arc<T>);
                    impl<T: Authorization>
                        tonic::server::UnaryService<super::SetAuthorizationStateRequest>
                        for SetAuthorizationStateSvc<T>
                    {
                        type Response = super::SetAuthorizationStateResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::SetAuthorizationStateRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Authorization>::set_authorization_state(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = SetAuthorizationStateSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Authorization/GetActiveModelRef" => {
                    #[allow(non_camel_case_types)]
                    struct GetActiveModelRefSvc<T: Authorization>(pub Arc<T>);
                    impl<T: Authorization> tonic::server::UnaryService<()> for GetActiveModelRefSvc<T> {
                        type Response = super::GetActiveModelRefResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(&mut self, request: tonic::Request<()>) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Authorization>::get_active_model_ref(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetActiveModelRefSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Authorization/SetActiveModel" => {
                    #[allow(non_camel_case_types)]
                    struct SetActiveModelSvc<T: Authorization>(pub Arc<T>);
                    impl<T: Authorization> tonic::server::UnaryService<super::SetActiveModelRequest>
                        for SetActiveModelSvc<T>
                    {
                        type Response = super::SetActiveModelResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::SetActiveModelRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Authorization>::set_active_model(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = SetActiveModelSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Authorization/ListActiveModelResourceTypes" => {
                    #[allow(non_camel_case_types)]
                    struct ListActiveModelResourceTypesSvc<T: Authorization>(pub Arc<T>);
                    impl<T: Authorization>
                        tonic::server::UnaryService<super::ListActiveModelResourceTypesRequest>
                        for ListActiveModelResourceTypesSvc<T>
                    {
                        type Response = super::ListActiveModelResourceTypesResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListActiveModelResourceTypesRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Authorization>::list_active_model_resource_types(
                                    &inner, request,
                                )
                                .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ListActiveModelResourceTypesSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => Box::pin(async move {
                    let mut response = http::Response::new(tonic::body::Body::default());
                    let headers = response.headers_mut();
                    headers.insert(
                        tonic::Status::GRPC_STATUS,
                        (tonic::Code::Unimplemented as i32).into(),
                    );
                    headers.insert(
                        http::header::CONTENT_TYPE,
                        tonic::metadata::GRPC_CONTENT_TYPE,
                    );
                    Ok(response)
                }),
            }
        }
    }
    impl<T> Clone for AuthorizationServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    /// Generated gRPC service name
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.Authorization";
    impl<T> tonic::server::NamedService for AuthorizationServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod cache_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    /** Cache models the shared Gestalt cache-provider protocol.
    */
    #[derive(Debug, Clone)]
    pub struct CacheClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl CacheClient<tonic::transport::Channel> {
        /// Attempt to create a new client by connecting to a given endpoint.
        pub async fn connect<D>(dst: D) -> Result<Self, tonic::transport::Error>
        where
            D: TryInto<tonic::transport::Endpoint>,
            D::Error: Into<StdError>,
        {
            let conn = tonic::transport::Endpoint::new(dst)?.connect().await?;
            Ok(Self::new(conn))
        }
    }
    impl<T> CacheClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::Body>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + std::marker::Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + std::marker::Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> CacheClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                    http::Request<tonic::body::Body>,
                    Response = http::Response<
                        <T as tonic::client::GrpcService<tonic::body::Body>>::ResponseBody,
                    >,
                >,
            <T as tonic::codegen::Service<http::Request<tonic::body::Body>>>::Error:
                Into<StdError> + std::marker::Send + std::marker::Sync,
        {
            CacheClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        pub async fn get(
            &mut self,
            request: impl tonic::IntoRequest<super::CacheGetRequest>,
        ) -> std::result::Result<tonic::Response<super::CacheGetResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Cache/Get");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Cache", "Get"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_many(
            &mut self,
            request: impl tonic::IntoRequest<super::CacheGetManyRequest>,
        ) -> std::result::Result<tonic::Response<super::CacheGetManyResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Cache/GetMany");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Cache", "GetMany"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn set(
            &mut self,
            request: impl tonic::IntoRequest<super::CacheSetRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Cache/Set");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Cache", "Set"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn set_many(
            &mut self,
            request: impl tonic::IntoRequest<super::CacheSetManyRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Cache/SetMany");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Cache", "SetMany"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn delete(
            &mut self,
            request: impl tonic::IntoRequest<super::CacheDeleteRequest>,
        ) -> std::result::Result<tonic::Response<super::CacheDeleteResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Cache/Delete");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Cache", "Delete"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn delete_many(
            &mut self,
            request: impl tonic::IntoRequest<super::CacheDeleteManyRequest>,
        ) -> std::result::Result<tonic::Response<super::CacheDeleteManyResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Cache/DeleteMany");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Cache", "DeleteMany"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn touch(
            &mut self,
            request: impl tonic::IntoRequest<super::CacheTouchRequest>,
        ) -> std::result::Result<tonic::Response<super::CacheTouchResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Cache/Touch");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Cache", "Touch"));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod cache_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with CacheServer.
    #[async_trait]
    pub trait Cache: std::marker::Send + std::marker::Sync + 'static {
        async fn get(
            &self,
            request: tonic::Request<super::CacheGetRequest>,
        ) -> std::result::Result<tonic::Response<super::CacheGetResponse>, tonic::Status>;
        ///
        async fn get_many(
            &self,
            request: tonic::Request<super::CacheGetManyRequest>,
        ) -> std::result::Result<tonic::Response<super::CacheGetManyResponse>, tonic::Status>;
        ///
        async fn set(
            &self,
            request: tonic::Request<super::CacheSetRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn set_many(
            &self,
            request: tonic::Request<super::CacheSetManyRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn delete(
            &self,
            request: tonic::Request<super::CacheDeleteRequest>,
        ) -> std::result::Result<tonic::Response<super::CacheDeleteResponse>, tonic::Status>;
        ///
        async fn delete_many(
            &self,
            request: tonic::Request<super::CacheDeleteManyRequest>,
        ) -> std::result::Result<tonic::Response<super::CacheDeleteManyResponse>, tonic::Status>;
        ///
        async fn touch(
            &self,
            request: tonic::Request<super::CacheTouchRequest>,
        ) -> std::result::Result<tonic::Response<super::CacheTouchResponse>, tonic::Status>;
    }
    /** Cache models the shared Gestalt cache-provider protocol.
    */
    #[derive(Debug)]
    pub struct CacheServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> CacheServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for CacheServer<T>
    where
        T: Cache,
        B: Body + std::marker::Send + 'static,
        B::Error: Into<StdError> + std::marker::Send + 'static,
    {
        type Response = http::Response<tonic::body::Body>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            match req.uri().path() {
                "/gestalt.provider.v1.Cache/Get" => {
                    #[allow(non_camel_case_types)]
                    struct GetSvc<T: Cache>(pub Arc<T>);
                    impl<T: Cache> tonic::server::UnaryService<super::CacheGetRequest> for GetSvc<T> {
                        type Response = super::CacheGetResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CacheGetRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as Cache>::get(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Cache/GetMany" => {
                    #[allow(non_camel_case_types)]
                    struct GetManySvc<T: Cache>(pub Arc<T>);
                    impl<T: Cache> tonic::server::UnaryService<super::CacheGetManyRequest> for GetManySvc<T> {
                        type Response = super::CacheGetManyResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CacheGetManyRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as Cache>::get_many(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetManySvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Cache/Set" => {
                    #[allow(non_camel_case_types)]
                    struct SetSvc<T: Cache>(pub Arc<T>);
                    impl<T: Cache> tonic::server::UnaryService<super::CacheSetRequest> for SetSvc<T> {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CacheSetRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as Cache>::set(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = SetSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Cache/SetMany" => {
                    #[allow(non_camel_case_types)]
                    struct SetManySvc<T: Cache>(pub Arc<T>);
                    impl<T: Cache> tonic::server::UnaryService<super::CacheSetManyRequest> for SetManySvc<T> {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CacheSetManyRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as Cache>::set_many(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = SetManySvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Cache/Delete" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteSvc<T: Cache>(pub Arc<T>);
                    impl<T: Cache> tonic::server::UnaryService<super::CacheDeleteRequest> for DeleteSvc<T> {
                        type Response = super::CacheDeleteResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CacheDeleteRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as Cache>::delete(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = DeleteSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Cache/DeleteMany" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteManySvc<T: Cache>(pub Arc<T>);
                    impl<T: Cache> tonic::server::UnaryService<super::CacheDeleteManyRequest> for DeleteManySvc<T> {
                        type Response = super::CacheDeleteManyResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CacheDeleteManyRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Cache>::delete_many(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = DeleteManySvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Cache/Touch" => {
                    #[allow(non_camel_case_types)]
                    struct TouchSvc<T: Cache>(pub Arc<T>);
                    impl<T: Cache> tonic::server::UnaryService<super::CacheTouchRequest> for TouchSvc<T> {
                        type Response = super::CacheTouchResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CacheTouchRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as Cache>::touch(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = TouchSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => Box::pin(async move {
                    let mut response = http::Response::new(tonic::body::Body::default());
                    let headers = response.headers_mut();
                    headers.insert(
                        tonic::Status::GRPC_STATUS,
                        (tonic::Code::Unimplemented as i32).into(),
                    );
                    headers.insert(
                        http::header::CONTENT_TYPE,
                        tonic::metadata::GRPC_CONTENT_TYPE,
                    );
                    Ok(response)
                }),
            }
        }
    }
    impl<T> Clone for CacheServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    /// Generated gRPC service name
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.Cache";
    impl<T> tonic::server::NamedService for CacheServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod external_credentials_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    ///
    #[derive(Debug, Clone)]
    pub struct ExternalCredentialsClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl ExternalCredentialsClient<tonic::transport::Channel> {
        /// Attempt to create a new client by connecting to a given endpoint.
        pub async fn connect<D>(dst: D) -> Result<Self, tonic::transport::Error>
        where
            D: TryInto<tonic::transport::Endpoint>,
            D::Error: Into<StdError>,
        {
            let conn = tonic::transport::Endpoint::new(dst)?.connect().await?;
            Ok(Self::new(conn))
        }
    }
    impl<T> ExternalCredentialsClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::Body>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + std::marker::Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + std::marker::Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> ExternalCredentialsClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                    http::Request<tonic::body::Body>,
                    Response = http::Response<
                        <T as tonic::client::GrpcService<tonic::body::Body>>::ResponseBody,
                    >,
                >,
            <T as tonic::codegen::Service<http::Request<tonic::body::Body>>>::Error:
                Into<StdError> + std::marker::Send + std::marker::Sync,
        {
            ExternalCredentialsClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        pub async fn create_credential(
            &mut self,
            request: impl tonic::IntoRequest<super::CreateExternalCredentialRequest>,
        ) -> std::result::Result<tonic::Response<super::ExternalCredential>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.ExternalCredentials/CreateCredential",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.ExternalCredentials",
                "CreateCredential",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn upsert_credential(
            &mut self,
            request: impl tonic::IntoRequest<super::UpsertExternalCredentialRequest>,
        ) -> std::result::Result<tonic::Response<super::ExternalCredential>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.ExternalCredentials/UpsertCredential",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.ExternalCredentials",
                "UpsertCredential",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_credential(
            &mut self,
            request: impl tonic::IntoRequest<super::GetExternalCredentialRequest>,
        ) -> std::result::Result<tonic::Response<super::ExternalCredential>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.ExternalCredentials/GetCredential",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.ExternalCredentials",
                "GetCredential",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_credentials(
            &mut self,
            request: impl tonic::IntoRequest<super::ListExternalCredentialsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListExternalCredentialsResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.ExternalCredentials/ListCredentials",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.ExternalCredentials",
                "ListCredentials",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn delete_credential(
            &mut self,
            request: impl tonic::IntoRequest<super::DeleteExternalCredentialRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.ExternalCredentials/DeleteCredential",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.ExternalCredentials",
                "DeleteCredential",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn validate_credential_config(
            &mut self,
            request: impl tonic::IntoRequest<super::ValidateExternalCredentialConfigRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.ExternalCredentials/ValidateCredentialConfig",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.ExternalCredentials",
                "ValidateCredentialConfig",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn resolve_credential(
            &mut self,
            request: impl tonic::IntoRequest<super::ResolveExternalCredentialRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ResolveExternalCredentialResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.ExternalCredentials/ResolveCredential",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.ExternalCredentials",
                "ResolveCredential",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn exchange_credential(
            &mut self,
            request: impl tonic::IntoRequest<super::ExchangeExternalCredentialRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ExchangeExternalCredentialResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.ExternalCredentials/ExchangeCredential",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.ExternalCredentials",
                "ExchangeCredential",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod external_credentials_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with ExternalCredentialsServer.
    #[async_trait]
    pub trait ExternalCredentials: std::marker::Send + std::marker::Sync + 'static {
        async fn create_credential(
            &self,
            request: tonic::Request<super::CreateExternalCredentialRequest>,
        ) -> std::result::Result<tonic::Response<super::ExternalCredential>, tonic::Status>;
        ///
        async fn upsert_credential(
            &self,
            request: tonic::Request<super::UpsertExternalCredentialRequest>,
        ) -> std::result::Result<tonic::Response<super::ExternalCredential>, tonic::Status>;
        ///
        async fn get_credential(
            &self,
            request: tonic::Request<super::GetExternalCredentialRequest>,
        ) -> std::result::Result<tonic::Response<super::ExternalCredential>, tonic::Status>;
        ///
        async fn list_credentials(
            &self,
            request: tonic::Request<super::ListExternalCredentialsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListExternalCredentialsResponse>,
            tonic::Status,
        >;
        ///
        async fn delete_credential(
            &self,
            request: tonic::Request<super::DeleteExternalCredentialRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn validate_credential_config(
            &self,
            request: tonic::Request<super::ValidateExternalCredentialConfigRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn resolve_credential(
            &self,
            request: tonic::Request<super::ResolveExternalCredentialRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ResolveExternalCredentialResponse>,
            tonic::Status,
        >;
        ///
        async fn exchange_credential(
            &self,
            request: tonic::Request<super::ExchangeExternalCredentialRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ExchangeExternalCredentialResponse>,
            tonic::Status,
        >;
    }
    ///
    #[derive(Debug)]
    pub struct ExternalCredentialsServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> ExternalCredentialsServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for ExternalCredentialsServer<T>
    where
        T: ExternalCredentials,
        B: Body + std::marker::Send + 'static,
        B::Error: Into<StdError> + std::marker::Send + 'static,
    {
        type Response = http::Response<tonic::body::Body>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            match req.uri().path() {
                "/gestalt.provider.v1.ExternalCredentials/CreateCredential" => {
                    #[allow(non_camel_case_types)]
                    struct CreateCredentialSvc<T: ExternalCredentials>(pub Arc<T>);
                    impl<T: ExternalCredentials>
                        tonic::server::UnaryService<super::CreateExternalCredentialRequest>
                        for CreateCredentialSvc<T>
                    {
                        type Response = super::ExternalCredential;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CreateExternalCredentialRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as ExternalCredentials>::create_credential(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = CreateCredentialSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.ExternalCredentials/UpsertCredential" => {
                    #[allow(non_camel_case_types)]
                    struct UpsertCredentialSvc<T: ExternalCredentials>(pub Arc<T>);
                    impl<T: ExternalCredentials>
                        tonic::server::UnaryService<super::UpsertExternalCredentialRequest>
                        for UpsertCredentialSvc<T>
                    {
                        type Response = super::ExternalCredential;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::UpsertExternalCredentialRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as ExternalCredentials>::upsert_credential(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = UpsertCredentialSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.ExternalCredentials/GetCredential" => {
                    #[allow(non_camel_case_types)]
                    struct GetCredentialSvc<T: ExternalCredentials>(pub Arc<T>);
                    impl<T: ExternalCredentials>
                        tonic::server::UnaryService<super::GetExternalCredentialRequest>
                        for GetCredentialSvc<T>
                    {
                        type Response = super::ExternalCredential;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetExternalCredentialRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as ExternalCredentials>::get_credential(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetCredentialSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.ExternalCredentials/ListCredentials" => {
                    #[allow(non_camel_case_types)]
                    struct ListCredentialsSvc<T: ExternalCredentials>(pub Arc<T>);
                    impl<T: ExternalCredentials>
                        tonic::server::UnaryService<super::ListExternalCredentialsRequest>
                        for ListCredentialsSvc<T>
                    {
                        type Response = super::ListExternalCredentialsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListExternalCredentialsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as ExternalCredentials>::list_credentials(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ListCredentialsSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.ExternalCredentials/DeleteCredential" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteCredentialSvc<T: ExternalCredentials>(pub Arc<T>);
                    impl<T: ExternalCredentials>
                        tonic::server::UnaryService<super::DeleteExternalCredentialRequest>
                        for DeleteCredentialSvc<T>
                    {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::DeleteExternalCredentialRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as ExternalCredentials>::delete_credential(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = DeleteCredentialSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.ExternalCredentials/ValidateCredentialConfig" => {
                    #[allow(non_camel_case_types)]
                    struct ValidateCredentialConfigSvc<T: ExternalCredentials>(pub Arc<T>);
                    impl<T: ExternalCredentials>
                        tonic::server::UnaryService<super::ValidateExternalCredentialConfigRequest>
                        for ValidateCredentialConfigSvc<T>
                    {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ValidateExternalCredentialConfigRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as ExternalCredentials>::validate_credential_config(
                                    &inner, request,
                                )
                                .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ValidateCredentialConfigSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.ExternalCredentials/ResolveCredential" => {
                    #[allow(non_camel_case_types)]
                    struct ResolveCredentialSvc<T: ExternalCredentials>(pub Arc<T>);
                    impl<T: ExternalCredentials>
                        tonic::server::UnaryService<super::ResolveExternalCredentialRequest>
                        for ResolveCredentialSvc<T>
                    {
                        type Response = super::ResolveExternalCredentialResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ResolveExternalCredentialRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as ExternalCredentials>::resolve_credential(&inner, request)
                                    .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ResolveCredentialSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.ExternalCredentials/ExchangeCredential" => {
                    #[allow(non_camel_case_types)]
                    struct ExchangeCredentialSvc<T: ExternalCredentials>(pub Arc<T>);
                    impl<T: ExternalCredentials>
                        tonic::server::UnaryService<super::ExchangeExternalCredentialRequest>
                        for ExchangeCredentialSvc<T>
                    {
                        type Response = super::ExchangeExternalCredentialResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ExchangeExternalCredentialRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as ExternalCredentials>::exchange_credential(&inner, request)
                                    .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ExchangeCredentialSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => Box::pin(async move {
                    let mut response = http::Response::new(tonic::body::Body::default());
                    let headers = response.headers_mut();
                    headers.insert(
                        tonic::Status::GRPC_STATUS,
                        (tonic::Code::Unimplemented as i32).into(),
                    );
                    headers.insert(
                        http::header::CONTENT_TYPE,
                        tonic::metadata::GRPC_CONTENT_TYPE,
                    );
                    Ok(response)
                }),
            }
        }
    }
    impl<T> Clone for ExternalCredentialsServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    /// Generated gRPC service name
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.ExternalCredentials";
    impl<T> tonic::server::NamedService for ExternalCredentialsServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod identity_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    /** Identity models the shared Gestalt authentication protocol.
    */
    #[derive(Debug, Clone)]
    pub struct IdentityClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl IdentityClient<tonic::transport::Channel> {
        /// Attempt to create a new client by connecting to a given endpoint.
        pub async fn connect<D>(dst: D) -> Result<Self, tonic::transport::Error>
        where
            D: TryInto<tonic::transport::Endpoint>,
            D::Error: Into<StdError>,
        {
            let conn = tonic::transport::Endpoint::new(dst)?.connect().await?;
            Ok(Self::new(conn))
        }
    }
    impl<T> IdentityClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::Body>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + std::marker::Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + std::marker::Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> IdentityClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                    http::Request<tonic::body::Body>,
                    Response = http::Response<
                        <T as tonic::client::GrpcService<tonic::body::Body>>::ResponseBody,
                    >,
                >,
            <T as tonic::codegen::Service<http::Request<tonic::body::Body>>>::Error:
                Into<StdError> + std::marker::Send + std::marker::Sync,
        {
            IdentityClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        pub async fn authorize(
            &mut self,
            request: impl tonic::IntoRequest<super::AuthorizeRequest>,
        ) -> std::result::Result<tonic::Response<super::AuthorizeResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Identity/Authorize");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Identity", "Authorize"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn token(
            &mut self,
            request: impl tonic::IntoRequest<super::TokenRequest>,
        ) -> std::result::Result<tonic::Response<super::TokenResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Identity/Token");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Identity", "Token"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn introspect(
            &mut self,
            request: impl tonic::IntoRequest<super::IntrospectRequest>,
        ) -> std::result::Result<tonic::Response<super::IntrospectResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Identity/Introspect");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Identity",
                "Introspect",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn user_info(
            &mut self,
            request: impl tonic::IntoRequest<super::UserInfoRequest>,
        ) -> std::result::Result<tonic::Response<super::UserInfoResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Identity/UserInfo");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Identity", "UserInfo"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_grants(
            &mut self,
            request: impl tonic::IntoRequest<super::ListGrantsRequest>,
        ) -> std::result::Result<tonic::Response<super::ListGrantsResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Identity/ListGrants");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Identity",
                "ListGrants",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_grant(
            &mut self,
            request: impl tonic::IntoRequest<super::GetGrantRequest>,
        ) -> std::result::Result<tonic::Response<super::GetGrantResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Identity/GetGrant");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Identity", "GetGrant"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn revoke_grant(
            &mut self,
            request: impl tonic::IntoRequest<super::RevokeGrantRequest>,
        ) -> std::result::Result<tonic::Response<super::RevokeGrantResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Identity/RevokeGrant");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Identity",
                "RevokeGrant",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod identity_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with IdentityServer.
    #[async_trait]
    pub trait Identity: std::marker::Send + std::marker::Sync + 'static {
        async fn authorize(
            &self,
            request: tonic::Request<super::AuthorizeRequest>,
        ) -> std::result::Result<tonic::Response<super::AuthorizeResponse>, tonic::Status>;
        ///
        async fn token(
            &self,
            request: tonic::Request<super::TokenRequest>,
        ) -> std::result::Result<tonic::Response<super::TokenResponse>, tonic::Status>;
        ///
        async fn introspect(
            &self,
            request: tonic::Request<super::IntrospectRequest>,
        ) -> std::result::Result<tonic::Response<super::IntrospectResponse>, tonic::Status>;
        ///
        async fn user_info(
            &self,
            request: tonic::Request<super::UserInfoRequest>,
        ) -> std::result::Result<tonic::Response<super::UserInfoResponse>, tonic::Status>;
        ///
        async fn list_grants(
            &self,
            request: tonic::Request<super::ListGrantsRequest>,
        ) -> std::result::Result<tonic::Response<super::ListGrantsResponse>, tonic::Status>;
        ///
        async fn get_grant(
            &self,
            request: tonic::Request<super::GetGrantRequest>,
        ) -> std::result::Result<tonic::Response<super::GetGrantResponse>, tonic::Status>;
        ///
        async fn revoke_grant(
            &self,
            request: tonic::Request<super::RevokeGrantRequest>,
        ) -> std::result::Result<tonic::Response<super::RevokeGrantResponse>, tonic::Status>;
    }
    /** Identity models the shared Gestalt authentication protocol.
    */
    #[derive(Debug)]
    pub struct IdentityServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> IdentityServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for IdentityServer<T>
    where
        T: Identity,
        B: Body + std::marker::Send + 'static,
        B::Error: Into<StdError> + std::marker::Send + 'static,
    {
        type Response = http::Response<tonic::body::Body>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            match req.uri().path() {
                "/gestalt.provider.v1.Identity/Authorize" => {
                    #[allow(non_camel_case_types)]
                    struct AuthorizeSvc<T: Identity>(pub Arc<T>);
                    impl<T: Identity> tonic::server::UnaryService<super::AuthorizeRequest> for AuthorizeSvc<T> {
                        type Response = super::AuthorizeResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::AuthorizeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Identity>::authorize(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = AuthorizeSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Identity/Token" => {
                    #[allow(non_camel_case_types)]
                    struct TokenSvc<T: Identity>(pub Arc<T>);
                    impl<T: Identity> tonic::server::UnaryService<super::TokenRequest> for TokenSvc<T> {
                        type Response = super::TokenResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::TokenRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as Identity>::token(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = TokenSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Identity/Introspect" => {
                    #[allow(non_camel_case_types)]
                    struct IntrospectSvc<T: Identity>(pub Arc<T>);
                    impl<T: Identity> tonic::server::UnaryService<super::IntrospectRequest> for IntrospectSvc<T> {
                        type Response = super::IntrospectResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::IntrospectRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Identity>::introspect(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = IntrospectSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Identity/UserInfo" => {
                    #[allow(non_camel_case_types)]
                    struct UserInfoSvc<T: Identity>(pub Arc<T>);
                    impl<T: Identity> tonic::server::UnaryService<super::UserInfoRequest> for UserInfoSvc<T> {
                        type Response = super::UserInfoResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::UserInfoRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Identity>::user_info(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = UserInfoSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Identity/ListGrants" => {
                    #[allow(non_camel_case_types)]
                    struct ListGrantsSvc<T: Identity>(pub Arc<T>);
                    impl<T: Identity> tonic::server::UnaryService<super::ListGrantsRequest> for ListGrantsSvc<T> {
                        type Response = super::ListGrantsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListGrantsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Identity>::list_grants(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ListGrantsSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Identity/GetGrant" => {
                    #[allow(non_camel_case_types)]
                    struct GetGrantSvc<T: Identity>(pub Arc<T>);
                    impl<T: Identity> tonic::server::UnaryService<super::GetGrantRequest> for GetGrantSvc<T> {
                        type Response = super::GetGrantResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetGrantRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Identity>::get_grant(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetGrantSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Identity/RevokeGrant" => {
                    #[allow(non_camel_case_types)]
                    struct RevokeGrantSvc<T: Identity>(pub Arc<T>);
                    impl<T: Identity> tonic::server::UnaryService<super::RevokeGrantRequest> for RevokeGrantSvc<T> {
                        type Response = super::RevokeGrantResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::RevokeGrantRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Identity>::revoke_grant(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = RevokeGrantSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => Box::pin(async move {
                    let mut response = http::Response::new(tonic::body::Body::default());
                    let headers = response.headers_mut();
                    headers.insert(
                        tonic::Status::GRPC_STATUS,
                        (tonic::Code::Unimplemented as i32).into(),
                    );
                    headers.insert(
                        http::header::CONTENT_TYPE,
                        tonic::metadata::GRPC_CONTENT_TYPE,
                    );
                    Ok(response)
                }),
            }
        }
    }
    impl<T> Clone for IdentityServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    /// Generated gRPC service name
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.Identity";
    impl<T> tonic::server::NamedService for IdentityServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod indexed_db_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    /** IndexedDB models the shared Gestalt IndexedDB-provider protocol.
    */
    #[derive(Debug, Clone)]
    pub struct IndexedDbClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl IndexedDbClient<tonic::transport::Channel> {
        /// Attempt to create a new client by connecting to a given endpoint.
        pub async fn connect<D>(dst: D) -> Result<Self, tonic::transport::Error>
        where
            D: TryInto<tonic::transport::Endpoint>,
            D::Error: Into<StdError>,
        {
            let conn = tonic::transport::Endpoint::new(dst)?.connect().await?;
            Ok(Self::new(conn))
        }
    }
    impl<T> IndexedDbClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::Body>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + std::marker::Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + std::marker::Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> IndexedDbClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                    http::Request<tonic::body::Body>,
                    Response = http::Response<
                        <T as tonic::client::GrpcService<tonic::body::Body>>::ResponseBody,
                    >,
                >,
            <T as tonic::codegen::Service<http::Request<tonic::body::Body>>>::Error:
                Into<StdError> + std::marker::Send + std::marker::Sync,
        {
            IndexedDbClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        pub async fn create_object_store(
            &mut self,
            request: impl tonic::IntoRequest<super::CreateObjectStoreRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.IndexedDB/CreateObjectStore",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IndexedDB",
                "CreateObjectStore",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn delete_object_store(
            &mut self,
            request: impl tonic::IntoRequest<super::DeleteObjectStoreRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.IndexedDB/DeleteObjectStore",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IndexedDB",
                "DeleteObjectStore",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn create_index(
            &mut self,
            request: impl tonic::IntoRequest<super::CreateIndexRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/CreateIndex");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IndexedDB",
                "CreateIndex",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn delete_index(
            &mut self,
            request: impl tonic::IntoRequest<super::DeleteIndexRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/DeleteIndex");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IndexedDB",
                "DeleteIndex",
            ));
            self.inner.unary(req, path, codec).await
        }
        /** Primary key CRUD
        */
        pub async fn get(
            &mut self,
            request: impl tonic::IntoRequest<super::ObjectStoreRequest>,
        ) -> std::result::Result<tonic::Response<super::RecordResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/Get");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.IndexedDB", "Get"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_key(
            &mut self,
            request: impl tonic::IntoRequest<super::ObjectStoreRequest>,
        ) -> std::result::Result<tonic::Response<super::KeyResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/GetKey");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.IndexedDB", "GetKey"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn add(
            &mut self,
            request: impl tonic::IntoRequest<super::RecordRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/Add");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.IndexedDB", "Add"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn put(
            &mut self,
            request: impl tonic::IntoRequest<super::RecordRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/Put");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.IndexedDB", "Put"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn delete(
            &mut self,
            request: impl tonic::IntoRequest<super::ObjectStoreRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/Delete");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.IndexedDB", "Delete"));
            self.inner.unary(req, path, codec).await
        }
        /** Bulk operations (with optional query)
        */
        pub async fn clear(
            &mut self,
            request: impl tonic::IntoRequest<super::ObjectStoreNameRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/Clear");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.IndexedDB", "Clear"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_all(
            &mut self,
            request: impl tonic::IntoRequest<super::ObjectStoreRangeRequest>,
        ) -> std::result::Result<tonic::Response<super::RecordsResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/GetAll");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.IndexedDB", "GetAll"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_all_keys(
            &mut self,
            request: impl tonic::IntoRequest<super::ObjectStoreRangeRequest>,
        ) -> std::result::Result<tonic::Response<super::KeysResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/GetAllKeys");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IndexedDB",
                "GetAllKeys",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn count(
            &mut self,
            request: impl tonic::IntoRequest<super::ObjectStoreRangeRequest>,
        ) -> std::result::Result<tonic::Response<super::CountResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/Count");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.IndexedDB", "Count"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn delete_range(
            &mut self,
            request: impl tonic::IntoRequest<super::ObjectStoreRangeRequest>,
        ) -> std::result::Result<tonic::Response<super::DeleteResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/DeleteRange");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IndexedDB",
                "DeleteRange",
            ));
            self.inner.unary(req, path, codec).await
        }
        /** Index queries
        */
        pub async fn index_get(
            &mut self,
            request: impl tonic::IntoRequest<super::IndexQueryRequest>,
        ) -> std::result::Result<tonic::Response<super::RecordResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/IndexGet");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.IndexedDB", "IndexGet"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn index_get_key(
            &mut self,
            request: impl tonic::IntoRequest<super::IndexQueryRequest>,
        ) -> std::result::Result<tonic::Response<super::KeyResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/IndexGetKey");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IndexedDB",
                "IndexGetKey",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn index_get_all(
            &mut self,
            request: impl tonic::IntoRequest<super::IndexQueryRequest>,
        ) -> std::result::Result<tonic::Response<super::RecordsResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/IndexGetAll");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IndexedDB",
                "IndexGetAll",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn index_get_all_keys(
            &mut self,
            request: impl tonic::IntoRequest<super::IndexQueryRequest>,
        ) -> std::result::Result<tonic::Response<super::KeysResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.IndexedDB/IndexGetAllKeys",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IndexedDB",
                "IndexGetAllKeys",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn index_count(
            &mut self,
            request: impl tonic::IntoRequest<super::IndexQueryRequest>,
        ) -> std::result::Result<tonic::Response<super::CountResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/IndexCount");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IndexedDB",
                "IndexCount",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn index_delete(
            &mut self,
            request: impl tonic::IntoRequest<super::IndexQueryRequest>,
        ) -> std::result::Result<tonic::Response<super::DeleteResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/IndexDelete");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IndexedDB",
                "IndexDelete",
            ));
            self.inner.unary(req, path, codec).await
        }
        /** Cursor iteration (bidirectional stream)
        */
        pub async fn open_cursor(
            &mut self,
            request: impl tonic::IntoStreamingRequest<Message = super::CursorClientMessage>,
        ) -> std::result::Result<
            tonic::Response<tonic::codec::Streaming<super::CursorResponse>>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/OpenCursor");
            let mut req = request.into_streaming_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IndexedDB",
                "OpenCursor",
            ));
            self.inner.streaming(req, path, codec).await
        }
        /** Transaction stream. The first client message must be
         BeginTransactionRequest. Stream close before commit aborts the transaction.
        */
        pub async fn transaction(
            &mut self,
            request: impl tonic::IntoStreamingRequest<Message = super::TransactionClientMessage>,
        ) -> std::result::Result<
            tonic::Response<tonic::codec::Streaming<super::TransactionServerMessage>>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.IndexedDB/Transaction");
            let mut req = request.into_streaming_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IndexedDB",
                "Transaction",
            ));
            self.inner.streaming(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod indexed_db_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with IndexedDbServer.
    #[async_trait]
    pub trait IndexedDb: std::marker::Send + std::marker::Sync + 'static {
        async fn create_object_store(
            &self,
            request: tonic::Request<super::CreateObjectStoreRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn delete_object_store(
            &self,
            request: tonic::Request<super::DeleteObjectStoreRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn create_index(
            &self,
            request: tonic::Request<super::CreateIndexRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn delete_index(
            &self,
            request: tonic::Request<super::DeleteIndexRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        /** Primary key CRUD
        */
        async fn get(
            &self,
            request: tonic::Request<super::ObjectStoreRequest>,
        ) -> std::result::Result<tonic::Response<super::RecordResponse>, tonic::Status>;
        ///
        async fn get_key(
            &self,
            request: tonic::Request<super::ObjectStoreRequest>,
        ) -> std::result::Result<tonic::Response<super::KeyResponse>, tonic::Status>;
        ///
        async fn add(
            &self,
            request: tonic::Request<super::RecordRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn put(
            &self,
            request: tonic::Request<super::RecordRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn delete(
            &self,
            request: tonic::Request<super::ObjectStoreRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        /** Bulk operations (with optional query)
        */
        async fn clear(
            &self,
            request: tonic::Request<super::ObjectStoreNameRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn get_all(
            &self,
            request: tonic::Request<super::ObjectStoreRangeRequest>,
        ) -> std::result::Result<tonic::Response<super::RecordsResponse>, tonic::Status>;
        ///
        async fn get_all_keys(
            &self,
            request: tonic::Request<super::ObjectStoreRangeRequest>,
        ) -> std::result::Result<tonic::Response<super::KeysResponse>, tonic::Status>;
        ///
        async fn count(
            &self,
            request: tonic::Request<super::ObjectStoreRangeRequest>,
        ) -> std::result::Result<tonic::Response<super::CountResponse>, tonic::Status>;
        ///
        async fn delete_range(
            &self,
            request: tonic::Request<super::ObjectStoreRangeRequest>,
        ) -> std::result::Result<tonic::Response<super::DeleteResponse>, tonic::Status>;
        /** Index queries
        */
        async fn index_get(
            &self,
            request: tonic::Request<super::IndexQueryRequest>,
        ) -> std::result::Result<tonic::Response<super::RecordResponse>, tonic::Status>;
        ///
        async fn index_get_key(
            &self,
            request: tonic::Request<super::IndexQueryRequest>,
        ) -> std::result::Result<tonic::Response<super::KeyResponse>, tonic::Status>;
        ///
        async fn index_get_all(
            &self,
            request: tonic::Request<super::IndexQueryRequest>,
        ) -> std::result::Result<tonic::Response<super::RecordsResponse>, tonic::Status>;
        ///
        async fn index_get_all_keys(
            &self,
            request: tonic::Request<super::IndexQueryRequest>,
        ) -> std::result::Result<tonic::Response<super::KeysResponse>, tonic::Status>;
        ///
        async fn index_count(
            &self,
            request: tonic::Request<super::IndexQueryRequest>,
        ) -> std::result::Result<tonic::Response<super::CountResponse>, tonic::Status>;
        ///
        async fn index_delete(
            &self,
            request: tonic::Request<super::IndexQueryRequest>,
        ) -> std::result::Result<tonic::Response<super::DeleteResponse>, tonic::Status>;
        /// Server streaming response type for the OpenCursor method.
        type OpenCursorStream: tonic::codegen::tokio_stream::Stream<
                Item = std::result::Result<super::CursorResponse, tonic::Status>,
            > + std::marker::Send
            + 'static;
        /** Cursor iteration (bidirectional stream)
        */
        async fn open_cursor(
            &self,
            request: tonic::Request<tonic::Streaming<super::CursorClientMessage>>,
        ) -> std::result::Result<tonic::Response<Self::OpenCursorStream>, tonic::Status>;
        /// Server streaming response type for the Transaction method.
        type TransactionStream: tonic::codegen::tokio_stream::Stream<
                Item = std::result::Result<super::TransactionServerMessage, tonic::Status>,
            > + std::marker::Send
            + 'static;
        /** Transaction stream. The first client message must be
         BeginTransactionRequest. Stream close before commit aborts the transaction.
        */
        async fn transaction(
            &self,
            request: tonic::Request<tonic::Streaming<super::TransactionClientMessage>>,
        ) -> std::result::Result<tonic::Response<Self::TransactionStream>, tonic::Status>;
    }
    /** IndexedDB models the shared Gestalt IndexedDB-provider protocol.
    */
    #[derive(Debug)]
    pub struct IndexedDbServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> IndexedDbServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for IndexedDbServer<T>
    where
        T: IndexedDb,
        B: Body + std::marker::Send + 'static,
        B::Error: Into<StdError> + std::marker::Send + 'static,
    {
        type Response = http::Response<tonic::body::Body>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            match req.uri().path() {
                "/gestalt.provider.v1.IndexedDB/CreateObjectStore" => {
                    #[allow(non_camel_case_types)]
                    struct CreateObjectStoreSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::CreateObjectStoreRequest>
                        for CreateObjectStoreSvc<T>
                    {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CreateObjectStoreRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as IndexedDb>::create_object_store(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = CreateObjectStoreSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/DeleteObjectStore" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteObjectStoreSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::DeleteObjectStoreRequest>
                        for DeleteObjectStoreSvc<T>
                    {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::DeleteObjectStoreRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as IndexedDb>::delete_object_store(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = DeleteObjectStoreSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/CreateIndex" => {
                    #[allow(non_camel_case_types)]
                    struct CreateIndexSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::CreateIndexRequest> for CreateIndexSvc<T> {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CreateIndexRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as IndexedDb>::create_index(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = CreateIndexSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/DeleteIndex" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteIndexSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::DeleteIndexRequest> for DeleteIndexSvc<T> {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::DeleteIndexRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as IndexedDb>::delete_index(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = DeleteIndexSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/Get" => {
                    #[allow(non_camel_case_types)]
                    struct GetSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::ObjectStoreRequest> for GetSvc<T> {
                        type Response = super::RecordResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ObjectStoreRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as IndexedDb>::get(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/GetKey" => {
                    #[allow(non_camel_case_types)]
                    struct GetKeySvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::ObjectStoreRequest> for GetKeySvc<T> {
                        type Response = super::KeyResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ObjectStoreRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as IndexedDb>::get_key(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetKeySvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/Add" => {
                    #[allow(non_camel_case_types)]
                    struct AddSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::RecordRequest> for AddSvc<T> {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::RecordRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as IndexedDb>::add(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = AddSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/Put" => {
                    #[allow(non_camel_case_types)]
                    struct PutSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::RecordRequest> for PutSvc<T> {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::RecordRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as IndexedDb>::put(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = PutSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/Delete" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::ObjectStoreRequest> for DeleteSvc<T> {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ObjectStoreRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as IndexedDb>::delete(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = DeleteSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/Clear" => {
                    #[allow(non_camel_case_types)]
                    struct ClearSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::ObjectStoreNameRequest> for ClearSvc<T> {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ObjectStoreNameRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as IndexedDb>::clear(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ClearSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/GetAll" => {
                    #[allow(non_camel_case_types)]
                    struct GetAllSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::ObjectStoreRangeRequest> for GetAllSvc<T> {
                        type Response = super::RecordsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ObjectStoreRangeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as IndexedDb>::get_all(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetAllSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/GetAllKeys" => {
                    #[allow(non_camel_case_types)]
                    struct GetAllKeysSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::ObjectStoreRangeRequest>
                        for GetAllKeysSvc<T>
                    {
                        type Response = super::KeysResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ObjectStoreRangeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as IndexedDb>::get_all_keys(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetAllKeysSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/Count" => {
                    #[allow(non_camel_case_types)]
                    struct CountSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::ObjectStoreRangeRequest> for CountSvc<T> {
                        type Response = super::CountResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ObjectStoreRangeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as IndexedDb>::count(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = CountSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/DeleteRange" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteRangeSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::ObjectStoreRangeRequest>
                        for DeleteRangeSvc<T>
                    {
                        type Response = super::DeleteResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ObjectStoreRangeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as IndexedDb>::delete_range(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = DeleteRangeSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/IndexGet" => {
                    #[allow(non_camel_case_types)]
                    struct IndexGetSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::IndexQueryRequest> for IndexGetSvc<T> {
                        type Response = super::RecordResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::IndexQueryRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as IndexedDb>::index_get(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = IndexGetSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/IndexGetKey" => {
                    #[allow(non_camel_case_types)]
                    struct IndexGetKeySvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::IndexQueryRequest> for IndexGetKeySvc<T> {
                        type Response = super::KeyResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::IndexQueryRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as IndexedDb>::index_get_key(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = IndexGetKeySvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/IndexGetAll" => {
                    #[allow(non_camel_case_types)]
                    struct IndexGetAllSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::IndexQueryRequest> for IndexGetAllSvc<T> {
                        type Response = super::RecordsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::IndexQueryRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as IndexedDb>::index_get_all(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = IndexGetAllSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/IndexGetAllKeys" => {
                    #[allow(non_camel_case_types)]
                    struct IndexGetAllKeysSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::IndexQueryRequest> for IndexGetAllKeysSvc<T> {
                        type Response = super::KeysResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::IndexQueryRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as IndexedDb>::index_get_all_keys(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = IndexGetAllKeysSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/IndexCount" => {
                    #[allow(non_camel_case_types)]
                    struct IndexCountSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::IndexQueryRequest> for IndexCountSvc<T> {
                        type Response = super::CountResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::IndexQueryRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as IndexedDb>::index_count(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = IndexCountSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/IndexDelete" => {
                    #[allow(non_camel_case_types)]
                    struct IndexDeleteSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::UnaryService<super::IndexQueryRequest> for IndexDeleteSvc<T> {
                        type Response = super::DeleteResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::IndexQueryRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as IndexedDb>::index_delete(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = IndexDeleteSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/OpenCursor" => {
                    #[allow(non_camel_case_types)]
                    struct OpenCursorSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb> tonic::server::StreamingService<super::CursorClientMessage>
                        for OpenCursorSvc<T>
                    {
                        type Response = super::CursorResponse;
                        type ResponseStream = T::OpenCursorStream;
                        type Future =
                            BoxFuture<tonic::Response<Self::ResponseStream>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<tonic::Streaming<super::CursorClientMessage>>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as IndexedDb>::open_cursor(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = OpenCursorSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.streaming(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.IndexedDB/Transaction" => {
                    #[allow(non_camel_case_types)]
                    struct TransactionSvc<T: IndexedDb>(pub Arc<T>);
                    impl<T: IndexedDb>
                        tonic::server::StreamingService<super::TransactionClientMessage>
                        for TransactionSvc<T>
                    {
                        type Response = super::TransactionServerMessage;
                        type ResponseStream = T::TransactionStream;
                        type Future =
                            BoxFuture<tonic::Response<Self::ResponseStream>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                tonic::Streaming<super::TransactionClientMessage>,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as IndexedDb>::transaction(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = TransactionSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.streaming(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => Box::pin(async move {
                    let mut response = http::Response::new(tonic::body::Body::default());
                    let headers = response.headers_mut();
                    headers.insert(
                        tonic::Status::GRPC_STATUS,
                        (tonic::Code::Unimplemented as i32).into(),
                    );
                    headers.insert(
                        http::header::CONTENT_TYPE,
                        tonic::metadata::GRPC_CONTENT_TYPE,
                    );
                    Ok(response)
                }),
            }
        }
    }
    impl<T> Clone for IndexedDbServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    /// Generated gRPC service name
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.IndexedDB";
    impl<T> tonic::server::NamedService for IndexedDbServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod provider_lifecycle_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    /** ProviderLifecycle is the common lifecycle protocol shared by every provider
     kind.
    */
    #[derive(Debug, Clone)]
    pub struct ProviderLifecycleClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl ProviderLifecycleClient<tonic::transport::Channel> {
        /// Attempt to create a new client by connecting to a given endpoint.
        pub async fn connect<D>(dst: D) -> Result<Self, tonic::transport::Error>
        where
            D: TryInto<tonic::transport::Endpoint>,
            D::Error: Into<StdError>,
        {
            let conn = tonic::transport::Endpoint::new(dst)?.connect().await?;
            Ok(Self::new(conn))
        }
    }
    impl<T> ProviderLifecycleClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::Body>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + std::marker::Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + std::marker::Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> ProviderLifecycleClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                    http::Request<tonic::body::Body>,
                    Response = http::Response<
                        <T as tonic::client::GrpcService<tonic::body::Body>>::ResponseBody,
                    >,
                >,
            <T as tonic::codegen::Service<http::Request<tonic::body::Body>>>::Error:
                Into<StdError> + std::marker::Send + std::marker::Sync,
        {
            ProviderLifecycleClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        ///
        pub async fn get_provider_identity(
            &mut self,
            request: impl tonic::IntoRequest<()>,
        ) -> std::result::Result<tonic::Response<super::ProviderIdentity>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.ProviderLifecycle/GetProviderIdentity",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.ProviderLifecycle",
                "GetProviderIdentity",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn configure_provider(
            &mut self,
            request: impl tonic::IntoRequest<super::ConfigureProviderRequest>,
        ) -> std::result::Result<tonic::Response<super::ConfigureProviderResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.ProviderLifecycle/ConfigureProvider",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.ProviderLifecycle",
                "ConfigureProvider",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn health_check(
            &mut self,
            request: impl tonic::IntoRequest<()>,
        ) -> std::result::Result<tonic::Response<super::HealthCheckResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.ProviderLifecycle/HealthCheck",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.ProviderLifecycle",
                "HealthCheck",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn start_provider(
            &mut self,
            request: impl tonic::IntoRequest<()>,
        ) -> std::result::Result<tonic::Response<super::StartRuntimeProviderResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.ProviderLifecycle/StartProvider",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.ProviderLifecycle",
                "StartProvider",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod provider_lifecycle_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with ProviderLifecycleServer.
    #[async_trait]
    pub trait ProviderLifecycle: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn get_provider_identity(
            &self,
            request: tonic::Request<()>,
        ) -> std::result::Result<tonic::Response<super::ProviderIdentity>, tonic::Status>;
        ///
        async fn configure_provider(
            &self,
            request: tonic::Request<super::ConfigureProviderRequest>,
        ) -> std::result::Result<tonic::Response<super::ConfigureProviderResponse>, tonic::Status>;
        ///
        async fn health_check(
            &self,
            request: tonic::Request<()>,
        ) -> std::result::Result<tonic::Response<super::HealthCheckResponse>, tonic::Status>;
        ///
        async fn start_provider(
            &self,
            request: tonic::Request<()>,
        ) -> std::result::Result<tonic::Response<super::StartRuntimeProviderResponse>, tonic::Status>;
    }
    /** ProviderLifecycle is the common lifecycle protocol shared by every provider
     kind.
    */
    #[derive(Debug)]
    pub struct ProviderLifecycleServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> ProviderLifecycleServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for ProviderLifecycleServer<T>
    where
        T: ProviderLifecycle,
        B: Body + std::marker::Send + 'static,
        B::Error: Into<StdError> + std::marker::Send + 'static,
    {
        type Response = http::Response<tonic::body::Body>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            match req.uri().path() {
                "/gestalt.provider.v1.ProviderLifecycle/GetProviderIdentity" => {
                    #[allow(non_camel_case_types)]
                    struct GetProviderIdentitySvc<T: ProviderLifecycle>(pub Arc<T>);
                    impl<T: ProviderLifecycle> tonic::server::UnaryService<()> for GetProviderIdentitySvc<T> {
                        type Response = super::ProviderIdentity;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(&mut self, request: tonic::Request<()>) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as ProviderLifecycle>::get_provider_identity(&inner, request)
                                    .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetProviderIdentitySvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.ProviderLifecycle/ConfigureProvider" => {
                    #[allow(non_camel_case_types)]
                    struct ConfigureProviderSvc<T: ProviderLifecycle>(pub Arc<T>);
                    impl<T: ProviderLifecycle>
                        tonic::server::UnaryService<super::ConfigureProviderRequest>
                        for ConfigureProviderSvc<T>
                    {
                        type Response = super::ConfigureProviderResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ConfigureProviderRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as ProviderLifecycle>::configure_provider(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ConfigureProviderSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.ProviderLifecycle/HealthCheck" => {
                    #[allow(non_camel_case_types)]
                    struct HealthCheckSvc<T: ProviderLifecycle>(pub Arc<T>);
                    impl<T: ProviderLifecycle> tonic::server::UnaryService<()> for HealthCheckSvc<T> {
                        type Response = super::HealthCheckResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(&mut self, request: tonic::Request<()>) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as ProviderLifecycle>::health_check(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = HealthCheckSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.ProviderLifecycle/StartProvider" => {
                    #[allow(non_camel_case_types)]
                    struct StartProviderSvc<T: ProviderLifecycle>(pub Arc<T>);
                    impl<T: ProviderLifecycle> tonic::server::UnaryService<()> for StartProviderSvc<T> {
                        type Response = super::StartRuntimeProviderResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(&mut self, request: tonic::Request<()>) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as ProviderLifecycle>::start_provider(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = StartProviderSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => Box::pin(async move {
                    let mut response = http::Response::new(tonic::body::Body::default());
                    let headers = response.headers_mut();
                    headers.insert(
                        tonic::Status::GRPC_STATUS,
                        (tonic::Code::Unimplemented as i32).into(),
                    );
                    headers.insert(
                        http::header::CONTENT_TYPE,
                        tonic::metadata::GRPC_CONTENT_TYPE,
                    );
                    Ok(response)
                }),
            }
        }
    }
    impl<T> Clone for ProviderLifecycleServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    /// Generated gRPC service name
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.ProviderLifecycle";
    impl<T> tonic::server::NamedService for ProviderLifecycleServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod runtime_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    ///
    #[derive(Debug, Clone)]
    pub struct RuntimeClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl RuntimeClient<tonic::transport::Channel> {
        /// Attempt to create a new client by connecting to a given endpoint.
        pub async fn connect<D>(dst: D) -> Result<Self, tonic::transport::Error>
        where
            D: TryInto<tonic::transport::Endpoint>,
            D::Error: Into<StdError>,
        {
            let conn = tonic::transport::Endpoint::new(dst)?.connect().await?;
            Ok(Self::new(conn))
        }
    }
    impl<T> RuntimeClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::Body>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + std::marker::Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + std::marker::Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> RuntimeClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                    http::Request<tonic::body::Body>,
                    Response = http::Response<
                        <T as tonic::client::GrpcService<tonic::body::Body>>::ResponseBody,
                    >,
                >,
            <T as tonic::codegen::Service<http::Request<tonic::body::Body>>>::Error:
                Into<StdError> + std::marker::Send + std::marker::Sync,
        {
            RuntimeClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        ///
        pub async fn get_support(
            &mut self,
            request: impl tonic::IntoRequest<()>,
        ) -> std::result::Result<tonic::Response<super::RuntimeSupport>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Runtime/GetSupport");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Runtime", "GetSupport"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn start_session(
            &mut self,
            request: impl tonic::IntoRequest<super::StartRuntimeSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::RuntimeSession>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Runtime/StartSession");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Runtime",
                "StartSession",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_session(
            &mut self,
            request: impl tonic::IntoRequest<super::GetRuntimeSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::RuntimeSession>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Runtime/GetSession");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Runtime", "GetSession"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_sessions(
            &mut self,
            request: impl tonic::IntoRequest<super::ListRuntimeSessionsRequest>,
        ) -> std::result::Result<tonic::Response<super::ListRuntimeSessionsResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Runtime/ListSessions");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Runtime",
                "ListSessions",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn stop_session(
            &mut self,
            request: impl tonic::IntoRequest<super::StopRuntimeSessionRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Runtime/StopSession");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Runtime",
                "StopSession",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn prepare_workspace(
            &mut self,
            request: impl tonic::IntoRequest<super::PrepareRuntimeWorkspaceRequest>,
        ) -> std::result::Result<
            tonic::Response<super::PrepareRuntimeWorkspaceResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Runtime/PrepareWorkspace",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Runtime",
                "PrepareWorkspace",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn remove_workspace(
            &mut self,
            request: impl tonic::IntoRequest<super::RemoveRuntimeWorkspaceRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Runtime/RemoveWorkspace",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Runtime",
                "RemoveWorkspace",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn start_app(
            &mut self,
            request: impl tonic::IntoRequest<super::StartHostedAppRequest>,
        ) -> std::result::Result<tonic::Response<super::HostedApp>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Runtime/StartApp");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Runtime", "StartApp"));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod runtime_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with RuntimeServer.
    #[async_trait]
    pub trait Runtime: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn get_support(
            &self,
            request: tonic::Request<()>,
        ) -> std::result::Result<tonic::Response<super::RuntimeSupport>, tonic::Status>;
        ///
        async fn start_session(
            &self,
            request: tonic::Request<super::StartRuntimeSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::RuntimeSession>, tonic::Status>;
        ///
        async fn get_session(
            &self,
            request: tonic::Request<super::GetRuntimeSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::RuntimeSession>, tonic::Status>;
        ///
        async fn list_sessions(
            &self,
            request: tonic::Request<super::ListRuntimeSessionsRequest>,
        ) -> std::result::Result<tonic::Response<super::ListRuntimeSessionsResponse>, tonic::Status>;
        ///
        async fn stop_session(
            &self,
            request: tonic::Request<super::StopRuntimeSessionRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn prepare_workspace(
            &self,
            request: tonic::Request<super::PrepareRuntimeWorkspaceRequest>,
        ) -> std::result::Result<
            tonic::Response<super::PrepareRuntimeWorkspaceResponse>,
            tonic::Status,
        >;
        ///
        async fn remove_workspace(
            &self,
            request: tonic::Request<super::RemoveRuntimeWorkspaceRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn start_app(
            &self,
            request: tonic::Request<super::StartHostedAppRequest>,
        ) -> std::result::Result<tonic::Response<super::HostedApp>, tonic::Status>;
    }
    ///
    #[derive(Debug)]
    pub struct RuntimeServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> RuntimeServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for RuntimeServer<T>
    where
        T: Runtime,
        B: Body + std::marker::Send + 'static,
        B::Error: Into<StdError> + std::marker::Send + 'static,
    {
        type Response = http::Response<tonic::body::Body>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            match req.uri().path() {
                "/gestalt.provider.v1.Runtime/GetSupport" => {
                    #[allow(non_camel_case_types)]
                    struct GetSupportSvc<T: Runtime>(pub Arc<T>);
                    impl<T: Runtime> tonic::server::UnaryService<()> for GetSupportSvc<T> {
                        type Response = super::RuntimeSupport;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(&mut self, request: tonic::Request<()>) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Runtime>::get_support(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetSupportSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Runtime/StartSession" => {
                    #[allow(non_camel_case_types)]
                    struct StartSessionSvc<T: Runtime>(pub Arc<T>);
                    impl<T: Runtime> tonic::server::UnaryService<super::StartRuntimeSessionRequest>
                        for StartSessionSvc<T>
                    {
                        type Response = super::RuntimeSession;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::StartRuntimeSessionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Runtime>::start_session(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = StartSessionSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Runtime/GetSession" => {
                    #[allow(non_camel_case_types)]
                    struct GetSessionSvc<T: Runtime>(pub Arc<T>);
                    impl<T: Runtime> tonic::server::UnaryService<super::GetRuntimeSessionRequest> for GetSessionSvc<T> {
                        type Response = super::RuntimeSession;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetRuntimeSessionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Runtime>::get_session(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetSessionSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Runtime/ListSessions" => {
                    #[allow(non_camel_case_types)]
                    struct ListSessionsSvc<T: Runtime>(pub Arc<T>);
                    impl<T: Runtime> tonic::server::UnaryService<super::ListRuntimeSessionsRequest>
                        for ListSessionsSvc<T>
                    {
                        type Response = super::ListRuntimeSessionsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListRuntimeSessionsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Runtime>::list_sessions(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ListSessionsSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Runtime/StopSession" => {
                    #[allow(non_camel_case_types)]
                    struct StopSessionSvc<T: Runtime>(pub Arc<T>);
                    impl<T: Runtime> tonic::server::UnaryService<super::StopRuntimeSessionRequest>
                        for StopSessionSvc<T>
                    {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::StopRuntimeSessionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Runtime>::stop_session(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = StopSessionSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Runtime/PrepareWorkspace" => {
                    #[allow(non_camel_case_types)]
                    struct PrepareWorkspaceSvc<T: Runtime>(pub Arc<T>);
                    impl<T: Runtime>
                        tonic::server::UnaryService<super::PrepareRuntimeWorkspaceRequest>
                        for PrepareWorkspaceSvc<T>
                    {
                        type Response = super::PrepareRuntimeWorkspaceResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::PrepareRuntimeWorkspaceRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Runtime>::prepare_workspace(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = PrepareWorkspaceSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Runtime/RemoveWorkspace" => {
                    #[allow(non_camel_case_types)]
                    struct RemoveWorkspaceSvc<T: Runtime>(pub Arc<T>);
                    impl<T: Runtime>
                        tonic::server::UnaryService<super::RemoveRuntimeWorkspaceRequest>
                        for RemoveWorkspaceSvc<T>
                    {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::RemoveRuntimeWorkspaceRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Runtime>::remove_workspace(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = RemoveWorkspaceSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Runtime/StartApp" => {
                    #[allow(non_camel_case_types)]
                    struct StartAppSvc<T: Runtime>(pub Arc<T>);
                    impl<T: Runtime> tonic::server::UnaryService<super::StartHostedAppRequest> for StartAppSvc<T> {
                        type Response = super::HostedApp;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::StartHostedAppRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Runtime>::start_app(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = StartAppSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => Box::pin(async move {
                    let mut response = http::Response::new(tonic::body::Body::default());
                    let headers = response.headers_mut();
                    headers.insert(
                        tonic::Status::GRPC_STATUS,
                        (tonic::Code::Unimplemented as i32).into(),
                    );
                    headers.insert(
                        http::header::CONTENT_TYPE,
                        tonic::metadata::GRPC_CONTENT_TYPE,
                    );
                    Ok(response)
                }),
            }
        }
    }
    impl<T> Clone for RuntimeServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    /// Generated gRPC service name
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.Runtime";
    impl<T> tonic::server::NamedService for RuntimeServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod s3_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    /** S3 models the shared Gestalt S3-provider protocol.
    */
    #[derive(Debug, Clone)]
    pub struct S3Client<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl S3Client<tonic::transport::Channel> {
        /// Attempt to create a new client by connecting to a given endpoint.
        pub async fn connect<D>(dst: D) -> Result<Self, tonic::transport::Error>
        where
            D: TryInto<tonic::transport::Endpoint>,
            D::Error: Into<StdError>,
        {
            let conn = tonic::transport::Endpoint::new(dst)?.connect().await?;
            Ok(Self::new(conn))
        }
    }
    impl<T> S3Client<T>
    where
        T: tonic::client::GrpcService<tonic::body::Body>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + std::marker::Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + std::marker::Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> S3Client<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                    http::Request<tonic::body::Body>,
                    Response = http::Response<
                        <T as tonic::client::GrpcService<tonic::body::Body>>::ResponseBody,
                    >,
                >,
            <T as tonic::codegen::Service<http::Request<tonic::body::Body>>>::Error:
                Into<StdError> + std::marker::Send + std::marker::Sync,
        {
            S3Client::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        pub async fn head_object(
            &mut self,
            request: impl tonic::IntoRequest<super::HeadObjectRequest>,
        ) -> std::result::Result<tonic::Response<super::HeadObjectResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.S3/HeadObject");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.S3", "HeadObject"));
            self.inner.unary(req, path, codec).await
        }
        /** The first response frame carries object metadata. All subsequent frames
         carry byte chunks. Zero-byte objects therefore emit exactly one frame.
        */
        pub async fn read_object(
            &mut self,
            request: impl tonic::IntoRequest<super::ReadObjectRequest>,
        ) -> std::result::Result<
            tonic::Response<tonic::codec::Streaming<super::ReadObjectChunk>>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.S3/ReadObject");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.S3", "ReadObject"));
            self.inner.server_streaming(req, path, codec).await
        }
        /** The first request frame must carry WriteObjectOpen metadata. All
         subsequent frames carry raw bytes. The response is emitted only after the
         object has been durably committed by the provider.
        */
        pub async fn write_object(
            &mut self,
            request: impl tonic::IntoStreamingRequest<Message = super::WriteObjectRequest>,
        ) -> std::result::Result<tonic::Response<super::WriteObjectResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.S3/WriteObject");
            let mut req = request.into_streaming_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.S3", "WriteObject"));
            self.inner.client_streaming(req, path, codec).await
        }
        ///
        pub async fn delete_object(
            &mut self,
            request: impl tonic::IntoRequest<super::DeleteObjectRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.S3/DeleteObject");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.S3", "DeleteObject"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_objects(
            &mut self,
            request: impl tonic::IntoRequest<super::ListObjectsRequest>,
        ) -> std::result::Result<tonic::Response<super::ListObjectsResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.S3/ListObjects");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.S3", "ListObjects"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn copy_object(
            &mut self,
            request: impl tonic::IntoRequest<super::CopyObjectRequest>,
        ) -> std::result::Result<tonic::Response<super::CopyObjectResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.S3/CopyObject");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.S3", "CopyObject"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn presign_object(
            &mut self,
            request: impl tonic::IntoRequest<super::PresignObjectRequest>,
        ) -> std::result::Result<tonic::Response<super::PresignObjectResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.S3/PresignObject");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.S3", "PresignObject"));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod s3_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with S3Server.
    #[async_trait]
    pub trait S3: std::marker::Send + std::marker::Sync + 'static {
        async fn head_object(
            &self,
            request: tonic::Request<super::HeadObjectRequest>,
        ) -> std::result::Result<tonic::Response<super::HeadObjectResponse>, tonic::Status>;
        /// Server streaming response type for the ReadObject method.
        type ReadObjectStream: tonic::codegen::tokio_stream::Stream<
                Item = std::result::Result<super::ReadObjectChunk, tonic::Status>,
            > + std::marker::Send
            + 'static;
        /** The first response frame carries object metadata. All subsequent frames
         carry byte chunks. Zero-byte objects therefore emit exactly one frame.
        */
        async fn read_object(
            &self,
            request: tonic::Request<super::ReadObjectRequest>,
        ) -> std::result::Result<tonic::Response<Self::ReadObjectStream>, tonic::Status>;
        /** The first request frame must carry WriteObjectOpen metadata. All
         subsequent frames carry raw bytes. The response is emitted only after the
         object has been durably committed by the provider.
        */
        async fn write_object(
            &self,
            request: tonic::Request<tonic::Streaming<super::WriteObjectRequest>>,
        ) -> std::result::Result<tonic::Response<super::WriteObjectResponse>, tonic::Status>;
        ///
        async fn delete_object(
            &self,
            request: tonic::Request<super::DeleteObjectRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn list_objects(
            &self,
            request: tonic::Request<super::ListObjectsRequest>,
        ) -> std::result::Result<tonic::Response<super::ListObjectsResponse>, tonic::Status>;
        ///
        async fn copy_object(
            &self,
            request: tonic::Request<super::CopyObjectRequest>,
        ) -> std::result::Result<tonic::Response<super::CopyObjectResponse>, tonic::Status>;
        ///
        async fn presign_object(
            &self,
            request: tonic::Request<super::PresignObjectRequest>,
        ) -> std::result::Result<tonic::Response<super::PresignObjectResponse>, tonic::Status>;
    }
    /** S3 models the shared Gestalt S3-provider protocol.
    */
    #[derive(Debug)]
    pub struct S3Server<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> S3Server<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for S3Server<T>
    where
        T: S3,
        B: Body + std::marker::Send + 'static,
        B::Error: Into<StdError> + std::marker::Send + 'static,
    {
        type Response = http::Response<tonic::body::Body>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            match req.uri().path() {
                "/gestalt.provider.v1.S3/HeadObject" => {
                    #[allow(non_camel_case_types)]
                    struct HeadObjectSvc<T: S3>(pub Arc<T>);
                    impl<T: S3> tonic::server::UnaryService<super::HeadObjectRequest> for HeadObjectSvc<T> {
                        type Response = super::HeadObjectResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::HeadObjectRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as S3>::head_object(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = HeadObjectSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.S3/ReadObject" => {
                    #[allow(non_camel_case_types)]
                    struct ReadObjectSvc<T: S3>(pub Arc<T>);
                    impl<T: S3> tonic::server::ServerStreamingService<super::ReadObjectRequest> for ReadObjectSvc<T> {
                        type Response = super::ReadObjectChunk;
                        type ResponseStream = T::ReadObjectStream;
                        type Future =
                            BoxFuture<tonic::Response<Self::ResponseStream>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ReadObjectRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as S3>::read_object(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ReadObjectSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.server_streaming(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.S3/WriteObject" => {
                    #[allow(non_camel_case_types)]
                    struct WriteObjectSvc<T: S3>(pub Arc<T>);
                    impl<T: S3> tonic::server::ClientStreamingService<super::WriteObjectRequest> for WriteObjectSvc<T> {
                        type Response = super::WriteObjectResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<tonic::Streaming<super::WriteObjectRequest>>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as S3>::write_object(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = WriteObjectSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.client_streaming(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.S3/DeleteObject" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteObjectSvc<T: S3>(pub Arc<T>);
                    impl<T: S3> tonic::server::UnaryService<super::DeleteObjectRequest> for DeleteObjectSvc<T> {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::DeleteObjectRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as S3>::delete_object(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = DeleteObjectSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.S3/ListObjects" => {
                    #[allow(non_camel_case_types)]
                    struct ListObjectsSvc<T: S3>(pub Arc<T>);
                    impl<T: S3> tonic::server::UnaryService<super::ListObjectsRequest> for ListObjectsSvc<T> {
                        type Response = super::ListObjectsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListObjectsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as S3>::list_objects(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ListObjectsSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.S3/CopyObject" => {
                    #[allow(non_camel_case_types)]
                    struct CopyObjectSvc<T: S3>(pub Arc<T>);
                    impl<T: S3> tonic::server::UnaryService<super::CopyObjectRequest> for CopyObjectSvc<T> {
                        type Response = super::CopyObjectResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CopyObjectRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move { <T as S3>::copy_object(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = CopyObjectSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.S3/PresignObject" => {
                    #[allow(non_camel_case_types)]
                    struct PresignObjectSvc<T: S3>(pub Arc<T>);
                    impl<T: S3> tonic::server::UnaryService<super::PresignObjectRequest> for PresignObjectSvc<T> {
                        type Response = super::PresignObjectResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::PresignObjectRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as S3>::presign_object(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = PresignObjectSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => Box::pin(async move {
                    let mut response = http::Response::new(tonic::body::Body::default());
                    let headers = response.headers_mut();
                    headers.insert(
                        tonic::Status::GRPC_STATUS,
                        (tonic::Code::Unimplemented as i32).into(),
                    );
                    headers.insert(
                        http::header::CONTENT_TYPE,
                        tonic::metadata::GRPC_CONTENT_TYPE,
                    );
                    Ok(response)
                }),
            }
        }
    }
    impl<T> Clone for S3Server<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    /// Generated gRPC service name
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.S3";
    impl<T> tonic::server::NamedService for S3Server<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod s3_object_access_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    /** S3ObjectAccess models host-mediated object access for plugin-scoped S3
     bindings. It is registered by gestaltd for apps and is not implemented by
     S3 providers.
    */
    #[derive(Debug, Clone)]
    pub struct S3ObjectAccessClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl S3ObjectAccessClient<tonic::transport::Channel> {
        /// Attempt to create a new client by connecting to a given endpoint.
        pub async fn connect<D>(dst: D) -> Result<Self, tonic::transport::Error>
        where
            D: TryInto<tonic::transport::Endpoint>,
            D::Error: Into<StdError>,
        {
            let conn = tonic::transport::Endpoint::new(dst)?.connect().await?;
            Ok(Self::new(conn))
        }
    }
    impl<T> S3ObjectAccessClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::Body>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + std::marker::Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + std::marker::Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> S3ObjectAccessClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                    http::Request<tonic::body::Body>,
                    Response = http::Response<
                        <T as tonic::client::GrpcService<tonic::body::Body>>::ResponseBody,
                    >,
                >,
            <T as tonic::codegen::Service<http::Request<tonic::body::Body>>>::Error:
                Into<StdError> + std::marker::Send + std::marker::Sync,
        {
            S3ObjectAccessClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        ///
        pub async fn create_object_access_url(
            &mut self,
            request: impl tonic::IntoRequest<super::CreateObjectAccessUrlRequest>,
        ) -> std::result::Result<tonic::Response<super::CreateObjectAccessUrlResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.S3ObjectAccess/CreateObjectAccessURL",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.S3ObjectAccess",
                "CreateObjectAccessURL",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod s3_object_access_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with S3ObjectAccessServer.
    #[async_trait]
    pub trait S3ObjectAccess: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn create_object_access_url(
            &self,
            request: tonic::Request<super::CreateObjectAccessUrlRequest>,
        ) -> std::result::Result<tonic::Response<super::CreateObjectAccessUrlResponse>, tonic::Status>;
    }
    /** S3ObjectAccess models host-mediated object access for plugin-scoped S3
     bindings. It is registered by gestaltd for apps and is not implemented by
     S3 providers.
    */
    #[derive(Debug)]
    pub struct S3ObjectAccessServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> S3ObjectAccessServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for S3ObjectAccessServer<T>
    where
        T: S3ObjectAccess,
        B: Body + std::marker::Send + 'static,
        B::Error: Into<StdError> + std::marker::Send + 'static,
    {
        type Response = http::Response<tonic::body::Body>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            match req.uri().path() {
                "/gestalt.provider.v1.S3ObjectAccess/CreateObjectAccessURL" => {
                    #[allow(non_camel_case_types)]
                    struct CreateObjectAccessURLSvc<T: S3ObjectAccess>(pub Arc<T>);
                    impl<T: S3ObjectAccess>
                        tonic::server::UnaryService<super::CreateObjectAccessUrlRequest>
                        for CreateObjectAccessURLSvc<T>
                    {
                        type Response = super::CreateObjectAccessUrlResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CreateObjectAccessUrlRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as S3ObjectAccess>::create_object_access_url(&inner, request)
                                    .await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = CreateObjectAccessURLSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => Box::pin(async move {
                    let mut response = http::Response::new(tonic::body::Body::default());
                    let headers = response.headers_mut();
                    headers.insert(
                        tonic::Status::GRPC_STATUS,
                        (tonic::Code::Unimplemented as i32).into(),
                    );
                    headers.insert(
                        http::header::CONTENT_TYPE,
                        tonic::metadata::GRPC_CONTENT_TYPE,
                    );
                    Ok(response)
                }),
            }
        }
    }
    impl<T> Clone for S3ObjectAccessServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    /// Generated gRPC service name
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.S3ObjectAccess";
    impl<T> tonic::server::NamedService for S3ObjectAccessServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod secrets_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    /** Secrets models the shared Gestalt secrets protocol.
    */
    #[derive(Debug, Clone)]
    pub struct SecretsClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl SecretsClient<tonic::transport::Channel> {
        /// Attempt to create a new client by connecting to a given endpoint.
        pub async fn connect<D>(dst: D) -> Result<Self, tonic::transport::Error>
        where
            D: TryInto<tonic::transport::Endpoint>,
            D::Error: Into<StdError>,
        {
            let conn = tonic::transport::Endpoint::new(dst)?.connect().await?;
            Ok(Self::new(conn))
        }
    }
    impl<T> SecretsClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::Body>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + std::marker::Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + std::marker::Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> SecretsClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                    http::Request<tonic::body::Body>,
                    Response = http::Response<
                        <T as tonic::client::GrpcService<tonic::body::Body>>::ResponseBody,
                    >,
                >,
            <T as tonic::codegen::Service<http::Request<tonic::body::Body>>>::Error:
                Into<StdError> + std::marker::Send + std::marker::Sync,
        {
            SecretsClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        ///
        pub async fn get_secret(
            &mut self,
            request: impl tonic::IntoRequest<super::GetSecretRequest>,
        ) -> std::result::Result<tonic::Response<super::GetSecretResponse>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Secrets/GetSecret");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Secrets", "GetSecret"));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod secrets_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with SecretsServer.
    #[async_trait]
    pub trait Secrets: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn get_secret(
            &self,
            request: tonic::Request<super::GetSecretRequest>,
        ) -> std::result::Result<tonic::Response<super::GetSecretResponse>, tonic::Status>;
    }
    /** Secrets models the shared Gestalt secrets protocol.
    */
    #[derive(Debug)]
    pub struct SecretsServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> SecretsServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for SecretsServer<T>
    where
        T: Secrets,
        B: Body + std::marker::Send + 'static,
        B::Error: Into<StdError> + std::marker::Send + 'static,
    {
        type Response = http::Response<tonic::body::Body>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            match req.uri().path() {
                "/gestalt.provider.v1.Secrets/GetSecret" => {
                    #[allow(non_camel_case_types)]
                    struct GetSecretSvc<T: Secrets>(pub Arc<T>);
                    impl<T: Secrets> tonic::server::UnaryService<super::GetSecretRequest> for GetSecretSvc<T> {
                        type Response = super::GetSecretResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetSecretRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Secrets>::get_secret(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetSecretSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => Box::pin(async move {
                    let mut response = http::Response::new(tonic::body::Body::default());
                    let headers = response.headers_mut();
                    headers.insert(
                        tonic::Status::GRPC_STATUS,
                        (tonic::Code::Unimplemented as i32).into(),
                    );
                    headers.insert(
                        http::header::CONTENT_TYPE,
                        tonic::metadata::GRPC_CONTENT_TYPE,
                    );
                    Ok(response)
                }),
            }
        }
    }
    impl<T> Clone for SecretsServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    /// Generated gRPC service name
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.Secrets";
    impl<T> tonic::server::NamedService for SecretsServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod test_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    /** Test models a minimal provider-kind-specific protocol used to verify new
     provider kind registration and lifecycle wiring.
    */
    #[derive(Debug, Clone)]
    pub struct TestClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl TestClient<tonic::transport::Channel> {
        /// Attempt to create a new client by connecting to a given endpoint.
        pub async fn connect<D>(dst: D) -> Result<Self, tonic::transport::Error>
        where
            D: TryInto<tonic::transport::Endpoint>,
            D::Error: Into<StdError>,
        {
            let conn = tonic::transport::Endpoint::new(dst)?.connect().await?;
            Ok(Self::new(conn))
        }
    }
    impl<T> TestClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::Body>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + std::marker::Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + std::marker::Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> TestClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                    http::Request<tonic::body::Body>,
                    Response = http::Response<
                        <T as tonic::client::GrpcService<tonic::body::Body>>::ResponseBody,
                    >,
                >,
            <T as tonic::codegen::Service<http::Request<tonic::body::Body>>>::Error:
                Into<StdError> + std::marker::Send + std::marker::Sync,
        {
            TestClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        ///
        pub async fn hello_world(
            &mut self,
            request: impl tonic::IntoRequest<super::HelloWorldRequest>,
        ) -> std::result::Result<tonic::Response<super::HelloWorldResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Test/HelloWorld");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Test", "HelloWorld"));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod test_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with TestServer.
    #[async_trait]
    pub trait Test: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn hello_world(
            &self,
            request: tonic::Request<super::HelloWorldRequest>,
        ) -> std::result::Result<tonic::Response<super::HelloWorldResponse>, tonic::Status>;
    }
    /** Test models a minimal provider-kind-specific protocol used to verify new
     provider kind registration and lifecycle wiring.
    */
    #[derive(Debug)]
    pub struct TestServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> TestServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for TestServer<T>
    where
        T: Test,
        B: Body + std::marker::Send + 'static,
        B::Error: Into<StdError> + std::marker::Send + 'static,
    {
        type Response = http::Response<tonic::body::Body>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            match req.uri().path() {
                "/gestalt.provider.v1.Test/HelloWorld" => {
                    #[allow(non_camel_case_types)]
                    struct HelloWorldSvc<T: Test>(pub Arc<T>);
                    impl<T: Test> tonic::server::UnaryService<super::HelloWorldRequest> for HelloWorldSvc<T> {
                        type Response = super::HelloWorldResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::HelloWorldRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Test>::hello_world(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = HelloWorldSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => Box::pin(async move {
                    let mut response = http::Response::new(tonic::body::Body::default());
                    let headers = response.headers_mut();
                    headers.insert(
                        tonic::Status::GRPC_STATUS,
                        (tonic::Code::Unimplemented as i32).into(),
                    );
                    headers.insert(
                        http::header::CONTENT_TYPE,
                        tonic::metadata::GRPC_CONTENT_TYPE,
                    );
                    Ok(response)
                }),
            }
        }
    }
    impl<T> Clone for TestServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    /// Generated gRPC service name
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.Test";
    impl<T> tonic::server::NamedService for TestServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod workflow_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    ///
    #[derive(Debug, Clone)]
    pub struct WorkflowClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl WorkflowClient<tonic::transport::Channel> {
        /// Attempt to create a new client by connecting to a given endpoint.
        pub async fn connect<D>(dst: D) -> Result<Self, tonic::transport::Error>
        where
            D: TryInto<tonic::transport::Endpoint>,
            D::Error: Into<StdError>,
        {
            let conn = tonic::transport::Endpoint::new(dst)?.connect().await?;
            Ok(Self::new(conn))
        }
    }
    impl<T> WorkflowClient<T>
    where
        T: tonic::client::GrpcService<tonic::body::Body>,
        T::Error: Into<StdError>,
        T::ResponseBody: Body<Data = Bytes> + std::marker::Send + 'static,
        <T::ResponseBody as Body>::Error: Into<StdError> + std::marker::Send,
    {
        pub fn new(inner: T) -> Self {
            let inner = tonic::client::Grpc::new(inner);
            Self { inner }
        }
        pub fn with_origin(inner: T, origin: Uri) -> Self {
            let inner = tonic::client::Grpc::with_origin(inner, origin);
            Self { inner }
        }
        pub fn with_interceptor<F>(
            inner: T,
            interceptor: F,
        ) -> WorkflowClient<InterceptedService<T, F>>
        where
            F: tonic::service::Interceptor,
            T::ResponseBody: Default,
            T: tonic::codegen::Service<
                    http::Request<tonic::body::Body>,
                    Response = http::Response<
                        <T as tonic::client::GrpcService<tonic::body::Body>>::ResponseBody,
                    >,
                >,
            <T as tonic::codegen::Service<http::Request<tonic::body::Body>>>::Error:
                Into<StdError> + std::marker::Send + std::marker::Sync,
        {
            WorkflowClient::new(InterceptedService::new(inner, interceptor))
        }
        /// Compress requests with the given encoding.
        ///
        /// This requires the server to support it otherwise it might respond with an
        /// error.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.send_compressed(encoding);
            self
        }
        /// Enable decompressing responses.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.inner = self.inner.accept_compressed(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_decoding_message_size(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.inner = self.inner.max_encoding_message_size(limit);
            self
        }
        ///
        pub async fn apply_definition(
            &mut self,
            request: impl tonic::IntoRequest<super::ApplyWorkflowProviderDefinitionRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowDefinition>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Workflow/ApplyDefinition",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Workflow",
                "ApplyDefinition",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_definition(
            &mut self,
            request: impl tonic::IntoRequest<super::GetWorkflowProviderDefinitionRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowDefinition>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Workflow/GetDefinition");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Workflow",
                "GetDefinition",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_definitions(
            &mut self,
            request: impl tonic::IntoRequest<super::ListWorkflowProviderDefinitionsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListWorkflowProviderDefinitionsResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Workflow/ListDefinitions",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Workflow",
                "ListDefinitions",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn set_definition_paused(
            &mut self,
            request: impl tonic::IntoRequest<super::SetWorkflowProviderDefinitionPausedRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowDefinition>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Workflow/SetDefinitionPaused",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Workflow",
                "SetDefinitionPaused",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn set_activation_paused(
            &mut self,
            request: impl tonic::IntoRequest<super::SetWorkflowProviderActivationPausedRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowDefinition>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Workflow/SetActivationPaused",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Workflow",
                "SetActivationPaused",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn delete_definition(
            &mut self,
            request: impl tonic::IntoRequest<super::DeleteWorkflowProviderDefinitionRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Workflow/DeleteDefinition",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Workflow",
                "DeleteDefinition",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn start_run(
            &mut self,
            request: impl tonic::IntoRequest<super::StartWorkflowProviderRunRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowRun>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Workflow/StartRun");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Workflow", "StartRun"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_runs(
            &mut self,
            request: impl tonic::IntoRequest<super::ListWorkflowProviderRunsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListWorkflowProviderRunsResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Workflow/ListRuns");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Workflow", "ListRuns"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_run(
            &mut self,
            request: impl tonic::IntoRequest<super::GetWorkflowProviderRunRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowRun>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Workflow/GetRun");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Workflow", "GetRun"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_run_events(
            &mut self,
            request: impl tonic::IntoRequest<super::GetWorkflowProviderRunEventsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::GetWorkflowProviderRunEventsResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Workflow/GetRunEvents");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Workflow",
                "GetRunEvents",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_run_output(
            &mut self,
            request: impl tonic::IntoRequest<super::GetWorkflowProviderRunOutputRequest>,
        ) -> std::result::Result<
            tonic::Response<super::GetWorkflowProviderRunOutputResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Workflow/GetRunOutput");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Workflow",
                "GetRunOutput",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn cancel_run(
            &mut self,
            request: impl tonic::IntoRequest<super::CancelWorkflowProviderRunRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowRun>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Workflow/CancelRun");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Workflow", "CancelRun"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn signal_run(
            &mut self,
            request: impl tonic::IntoRequest<super::SignalWorkflowProviderRunRequest>,
        ) -> std::result::Result<tonic::Response<super::SignalWorkflowRunResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Workflow/SignalRun");
            let mut req = request.into_request();
            req.extensions_mut()
                .insert(GrpcMethod::new("gestalt.provider.v1.Workflow", "SignalRun"));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn signal_or_start_run(
            &mut self,
            request: impl tonic::IntoRequest<super::SignalOrStartWorkflowProviderRunRequest>,
        ) -> std::result::Result<tonic::Response<super::SignalWorkflowRunResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.Workflow/SignalOrStartRun",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Workflow",
                "SignalOrStartRun",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn deliver_event(
            &mut self,
            request: impl tonic::IntoRequest<super::DeliverWorkflowProviderEventRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowEvent>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.Workflow/DeliverEvent");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.Workflow",
                "DeliverEvent",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod workflow_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with WorkflowServer.
    #[async_trait]
    pub trait Workflow: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn apply_definition(
            &self,
            request: tonic::Request<super::ApplyWorkflowProviderDefinitionRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowDefinition>, tonic::Status>;
        ///
        async fn get_definition(
            &self,
            request: tonic::Request<super::GetWorkflowProviderDefinitionRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowDefinition>, tonic::Status>;
        ///
        async fn list_definitions(
            &self,
            request: tonic::Request<super::ListWorkflowProviderDefinitionsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListWorkflowProviderDefinitionsResponse>,
            tonic::Status,
        >;
        ///
        async fn set_definition_paused(
            &self,
            request: tonic::Request<super::SetWorkflowProviderDefinitionPausedRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowDefinition>, tonic::Status>;
        ///
        async fn set_activation_paused(
            &self,
            request: tonic::Request<super::SetWorkflowProviderActivationPausedRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowDefinition>, tonic::Status>;
        ///
        async fn delete_definition(
            &self,
            request: tonic::Request<super::DeleteWorkflowProviderDefinitionRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn start_run(
            &self,
            request: tonic::Request<super::StartWorkflowProviderRunRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowRun>, tonic::Status>;
        ///
        async fn list_runs(
            &self,
            request: tonic::Request<super::ListWorkflowProviderRunsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListWorkflowProviderRunsResponse>,
            tonic::Status,
        >;
        ///
        async fn get_run(
            &self,
            request: tonic::Request<super::GetWorkflowProviderRunRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowRun>, tonic::Status>;
        ///
        async fn get_run_events(
            &self,
            request: tonic::Request<super::GetWorkflowProviderRunEventsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::GetWorkflowProviderRunEventsResponse>,
            tonic::Status,
        >;
        ///
        async fn get_run_output(
            &self,
            request: tonic::Request<super::GetWorkflowProviderRunOutputRequest>,
        ) -> std::result::Result<
            tonic::Response<super::GetWorkflowProviderRunOutputResponse>,
            tonic::Status,
        >;
        ///
        async fn cancel_run(
            &self,
            request: tonic::Request<super::CancelWorkflowProviderRunRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowRun>, tonic::Status>;
        ///
        async fn signal_run(
            &self,
            request: tonic::Request<super::SignalWorkflowProviderRunRequest>,
        ) -> std::result::Result<tonic::Response<super::SignalWorkflowRunResponse>, tonic::Status>;
        ///
        async fn signal_or_start_run(
            &self,
            request: tonic::Request<super::SignalOrStartWorkflowProviderRunRequest>,
        ) -> std::result::Result<tonic::Response<super::SignalWorkflowRunResponse>, tonic::Status>;
        ///
        async fn deliver_event(
            &self,
            request: tonic::Request<super::DeliverWorkflowProviderEventRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowEvent>, tonic::Status>;
    }
    ///
    #[derive(Debug)]
    pub struct WorkflowServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> WorkflowServer<T> {
        pub fn new(inner: T) -> Self {
            Self::from_arc(Arc::new(inner))
        }
        pub fn from_arc(inner: Arc<T>) -> Self {
            Self {
                inner,
                accept_compression_encodings: Default::default(),
                send_compression_encodings: Default::default(),
                max_decoding_message_size: None,
                max_encoding_message_size: None,
            }
        }
        pub fn with_interceptor<F>(inner: T, interceptor: F) -> InterceptedService<Self, F>
        where
            F: tonic::service::Interceptor,
        {
            InterceptedService::new(Self::new(inner), interceptor)
        }
        /// Enable decompressing requests with the given encoding.
        #[must_use]
        pub fn accept_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.accept_compression_encodings.enable(encoding);
            self
        }
        /// Compress responses with the given encoding, if the client supports it.
        #[must_use]
        pub fn send_compressed(mut self, encoding: CompressionEncoding) -> Self {
            self.send_compression_encodings.enable(encoding);
            self
        }
        /// Limits the maximum size of a decoded message.
        ///
        /// Default: `4MB`
        #[must_use]
        pub fn max_decoding_message_size(mut self, limit: usize) -> Self {
            self.max_decoding_message_size = Some(limit);
            self
        }
        /// Limits the maximum size of an encoded message.
        ///
        /// Default: `usize::MAX`
        #[must_use]
        pub fn max_encoding_message_size(mut self, limit: usize) -> Self {
            self.max_encoding_message_size = Some(limit);
            self
        }
    }
    impl<T, B> tonic::codegen::Service<http::Request<B>> for WorkflowServer<T>
    where
        T: Workflow,
        B: Body + std::marker::Send + 'static,
        B::Error: Into<StdError> + std::marker::Send + 'static,
    {
        type Response = http::Response<tonic::body::Body>;
        type Error = std::convert::Infallible;
        type Future = BoxFuture<Self::Response, Self::Error>;
        fn poll_ready(
            &mut self,
            _cx: &mut Context<'_>,
        ) -> Poll<std::result::Result<(), Self::Error>> {
            Poll::Ready(Ok(()))
        }
        fn call(&mut self, req: http::Request<B>) -> Self::Future {
            match req.uri().path() {
                "/gestalt.provider.v1.Workflow/ApplyDefinition" => {
                    #[allow(non_camel_case_types)]
                    struct ApplyDefinitionSvc<T: Workflow>(pub Arc<T>);
                    impl<T: Workflow>
                        tonic::server::UnaryService<super::ApplyWorkflowProviderDefinitionRequest>
                        for ApplyDefinitionSvc<T>
                    {
                        type Response = super::WorkflowDefinition;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ApplyWorkflowProviderDefinitionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Workflow>::apply_definition(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ApplyDefinitionSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Workflow/GetDefinition" => {
                    #[allow(non_camel_case_types)]
                    struct GetDefinitionSvc<T: Workflow>(pub Arc<T>);
                    impl<T: Workflow>
                        tonic::server::UnaryService<super::GetWorkflowProviderDefinitionRequest>
                        for GetDefinitionSvc<T>
                    {
                        type Response = super::WorkflowDefinition;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetWorkflowProviderDefinitionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Workflow>::get_definition(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetDefinitionSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Workflow/ListDefinitions" => {
                    #[allow(non_camel_case_types)]
                    struct ListDefinitionsSvc<T: Workflow>(pub Arc<T>);
                    impl<T: Workflow>
                        tonic::server::UnaryService<super::ListWorkflowProviderDefinitionsRequest>
                        for ListDefinitionsSvc<T>
                    {
                        type Response = super::ListWorkflowProviderDefinitionsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListWorkflowProviderDefinitionsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Workflow>::list_definitions(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ListDefinitionsSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Workflow/SetDefinitionPaused" => {
                    #[allow(non_camel_case_types)]
                    struct SetDefinitionPausedSvc<T: Workflow>(pub Arc<T>);
                    impl<T: Workflow>
                        tonic::server::UnaryService<
                            super::SetWorkflowProviderDefinitionPausedRequest,
                        > for SetDefinitionPausedSvc<T>
                    {
                        type Response = super::WorkflowDefinition;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::SetWorkflowProviderDefinitionPausedRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Workflow>::set_definition_paused(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = SetDefinitionPausedSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Workflow/SetActivationPaused" => {
                    #[allow(non_camel_case_types)]
                    struct SetActivationPausedSvc<T: Workflow>(pub Arc<T>);
                    impl<T: Workflow>
                        tonic::server::UnaryService<
                            super::SetWorkflowProviderActivationPausedRequest,
                        > for SetActivationPausedSvc<T>
                    {
                        type Response = super::WorkflowDefinition;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::SetWorkflowProviderActivationPausedRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Workflow>::set_activation_paused(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = SetActivationPausedSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Workflow/DeleteDefinition" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteDefinitionSvc<T: Workflow>(pub Arc<T>);
                    impl<T: Workflow>
                        tonic::server::UnaryService<super::DeleteWorkflowProviderDefinitionRequest>
                        for DeleteDefinitionSvc<T>
                    {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::DeleteWorkflowProviderDefinitionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Workflow>::delete_definition(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = DeleteDefinitionSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Workflow/StartRun" => {
                    #[allow(non_camel_case_types)]
                    struct StartRunSvc<T: Workflow>(pub Arc<T>);
                    impl<T: Workflow>
                        tonic::server::UnaryService<super::StartWorkflowProviderRunRequest>
                        for StartRunSvc<T>
                    {
                        type Response = super::WorkflowRun;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::StartWorkflowProviderRunRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Workflow>::start_run(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = StartRunSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Workflow/ListRuns" => {
                    #[allow(non_camel_case_types)]
                    struct ListRunsSvc<T: Workflow>(pub Arc<T>);
                    impl<T: Workflow>
                        tonic::server::UnaryService<super::ListWorkflowProviderRunsRequest>
                        for ListRunsSvc<T>
                    {
                        type Response = super::ListWorkflowProviderRunsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListWorkflowProviderRunsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Workflow>::list_runs(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ListRunsSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Workflow/GetRun" => {
                    #[allow(non_camel_case_types)]
                    struct GetRunSvc<T: Workflow>(pub Arc<T>);
                    impl<T: Workflow>
                        tonic::server::UnaryService<super::GetWorkflowProviderRunRequest>
                        for GetRunSvc<T>
                    {
                        type Response = super::WorkflowRun;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetWorkflowProviderRunRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Workflow>::get_run(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetRunSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Workflow/GetRunEvents" => {
                    #[allow(non_camel_case_types)]
                    struct GetRunEventsSvc<T: Workflow>(pub Arc<T>);
                    impl<T: Workflow>
                        tonic::server::UnaryService<super::GetWorkflowProviderRunEventsRequest>
                        for GetRunEventsSvc<T>
                    {
                        type Response = super::GetWorkflowProviderRunEventsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetWorkflowProviderRunEventsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Workflow>::get_run_events(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetRunEventsSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Workflow/GetRunOutput" => {
                    #[allow(non_camel_case_types)]
                    struct GetRunOutputSvc<T: Workflow>(pub Arc<T>);
                    impl<T: Workflow>
                        tonic::server::UnaryService<super::GetWorkflowProviderRunOutputRequest>
                        for GetRunOutputSvc<T>
                    {
                        type Response = super::GetWorkflowProviderRunOutputResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetWorkflowProviderRunOutputRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Workflow>::get_run_output(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = GetRunOutputSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Workflow/CancelRun" => {
                    #[allow(non_camel_case_types)]
                    struct CancelRunSvc<T: Workflow>(pub Arc<T>);
                    impl<T: Workflow>
                        tonic::server::UnaryService<super::CancelWorkflowProviderRunRequest>
                        for CancelRunSvc<T>
                    {
                        type Response = super::WorkflowRun;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CancelWorkflowProviderRunRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Workflow>::cancel_run(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = CancelRunSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Workflow/SignalRun" => {
                    #[allow(non_camel_case_types)]
                    struct SignalRunSvc<T: Workflow>(pub Arc<T>);
                    impl<T: Workflow>
                        tonic::server::UnaryService<super::SignalWorkflowProviderRunRequest>
                        for SignalRunSvc<T>
                    {
                        type Response = super::SignalWorkflowRunResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::SignalWorkflowProviderRunRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as Workflow>::signal_run(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = SignalRunSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Workflow/SignalOrStartRun" => {
                    #[allow(non_camel_case_types)]
                    struct SignalOrStartRunSvc<T: Workflow>(pub Arc<T>);
                    impl<T: Workflow>
                        tonic::server::UnaryService<super::SignalOrStartWorkflowProviderRunRequest>
                        for SignalOrStartRunSvc<T>
                    {
                        type Response = super::SignalWorkflowRunResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::SignalOrStartWorkflowProviderRunRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Workflow>::signal_or_start_run(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = SignalOrStartRunSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                "/gestalt.provider.v1.Workflow/DeliverEvent" => {
                    #[allow(non_camel_case_types)]
                    struct DeliverEventSvc<T: Workflow>(pub Arc<T>);
                    impl<T: Workflow>
                        tonic::server::UnaryService<super::DeliverWorkflowProviderEventRequest>
                        for DeliverEventSvc<T>
                    {
                        type Response = super::WorkflowEvent;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::DeliverWorkflowProviderEventRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as Workflow>::deliver_event(&inner, request).await
                            };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = DeliverEventSvc(inner);
                        let codec = tonic_prost::ProstCodec::default();
                        let mut grpc = tonic::server::Grpc::new(codec)
                            .apply_compression_config(
                                accept_compression_encodings,
                                send_compression_encodings,
                            )
                            .apply_max_message_size_config(
                                max_decoding_message_size,
                                max_encoding_message_size,
                            );
                        let res = grpc.unary(method, req).await;
                        Ok(res)
                    };
                    Box::pin(fut)
                }
                _ => Box::pin(async move {
                    let mut response = http::Response::new(tonic::body::Body::default());
                    let headers = response.headers_mut();
                    headers.insert(
                        tonic::Status::GRPC_STATUS,
                        (tonic::Code::Unimplemented as i32).into(),
                    );
                    headers.insert(
                        http::header::CONTENT_TYPE,
                        tonic::metadata::GRPC_CONTENT_TYPE,
                    );
                    Ok(response)
                }),
            }
        }
    }
    impl<T> Clone for WorkflowServer<T> {
        fn clone(&self) -> Self {
            let inner = self.inner.clone();
            Self {
                inner,
                accept_compression_encodings: self.accept_compression_encodings,
                send_compression_encodings: self.send_compression_encodings,
                max_decoding_message_size: self.max_decoding_message_size,
                max_encoding_message_size: self.max_encoding_message_size,
            }
        }
    }
    /// Generated gRPC service name
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.Workflow";
    impl<T> tonic::server::NamedService for WorkflowServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
