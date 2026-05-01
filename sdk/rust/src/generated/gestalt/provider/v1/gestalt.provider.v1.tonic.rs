// @generated
/// Generated client implementations.
pub mod integration_provider_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    /** IntegrationProvider models the shared Gestalt integration-provider protocol.
    */
    #[derive(Debug, Clone)]
    pub struct IntegrationProviderClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl IntegrationProviderClient<tonic::transport::Channel> {
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
    impl<T> IntegrationProviderClient<T>
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
        ) -> IntegrationProviderClient<InterceptedService<T, F>>
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
            IntegrationProviderClient::new(InterceptedService::new(inner, interceptor))
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
                "/gestalt.provider.v1.IntegrationProvider/GetMetadata",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IntegrationProvider",
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
                "/gestalt.provider.v1.IntegrationProvider/StartProvider",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IntegrationProvider",
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
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.IntegrationProvider/Execute",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IntegrationProvider",
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
                "/gestalt.provider.v1.IntegrationProvider/ResolveHTTPSubject",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IntegrationProvider",
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
                "/gestalt.provider.v1.IntegrationProvider/GetSessionCatalog",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IntegrationProvider",
                "GetSessionCatalog",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn post_connect(
            &mut self,
            request: impl tonic::IntoRequest<super::PostConnectRequest>,
        ) -> std::result::Result<tonic::Response<super::PostConnectResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.IntegrationProvider/PostConnect",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.IntegrationProvider",
                "PostConnect",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod integration_provider_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with IntegrationProviderServer.
    #[async_trait]
    pub trait IntegrationProvider: std::marker::Send + std::marker::Sync + 'static {
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
        ///
        async fn post_connect(
            &self,
            request: tonic::Request<super::PostConnectRequest>,
        ) -> std::result::Result<tonic::Response<super::PostConnectResponse>, tonic::Status>;
    }
    /** IntegrationProvider models the shared Gestalt integration-provider protocol.
    */
    #[derive(Debug)]
    pub struct IntegrationProviderServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> IntegrationProviderServer<T> {
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
    impl<T, B> tonic::codegen::Service<http::Request<B>> for IntegrationProviderServer<T>
    where
        T: IntegrationProvider,
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
                "/gestalt.provider.v1.IntegrationProvider/GetMetadata" => {
                    #[allow(non_camel_case_types)]
                    struct GetMetadataSvc<T: IntegrationProvider>(pub Arc<T>);
                    impl<T: IntegrationProvider> tonic::server::UnaryService<()> for GetMetadataSvc<T> {
                        type Response = super::ProviderMetadata;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(&mut self, request: tonic::Request<()>) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as IntegrationProvider>::get_metadata(&inner, request).await
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
                "/gestalt.provider.v1.IntegrationProvider/StartProvider" => {
                    #[allow(non_camel_case_types)]
                    struct StartProviderSvc<T: IntegrationProvider>(pub Arc<T>);
                    impl<T: IntegrationProvider>
                        tonic::server::UnaryService<super::StartProviderRequest>
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
                                <T as IntegrationProvider>::start_provider(&inner, request).await
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
                "/gestalt.provider.v1.IntegrationProvider/Execute" => {
                    #[allow(non_camel_case_types)]
                    struct ExecuteSvc<T: IntegrationProvider>(pub Arc<T>);
                    impl<T: IntegrationProvider> tonic::server::UnaryService<super::ExecuteRequest> for ExecuteSvc<T> {
                        type Response = super::OperationResult;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ExecuteRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as IntegrationProvider>::execute(&inner, request).await
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
                "/gestalt.provider.v1.IntegrationProvider/ResolveHTTPSubject" => {
                    #[allow(non_camel_case_types)]
                    struct ResolveHTTPSubjectSvc<T: IntegrationProvider>(pub Arc<T>);
                    impl<T: IntegrationProvider>
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
                                <T as IntegrationProvider>::resolve_http_subject(&inner, request)
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
                "/gestalt.provider.v1.IntegrationProvider/GetSessionCatalog" => {
                    #[allow(non_camel_case_types)]
                    struct GetSessionCatalogSvc<T: IntegrationProvider>(pub Arc<T>);
                    impl<T: IntegrationProvider>
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
                                <T as IntegrationProvider>::get_session_catalog(&inner, request)
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
                "/gestalt.provider.v1.IntegrationProvider/PostConnect" => {
                    #[allow(non_camel_case_types)]
                    struct PostConnectSvc<T: IntegrationProvider>(pub Arc<T>);
                    impl<T: IntegrationProvider>
                        tonic::server::UnaryService<super::PostConnectRequest>
                        for PostConnectSvc<T>
                    {
                        type Response = super::PostConnectResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::PostConnectRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as IntegrationProvider>::post_connect(&inner, request).await
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
                        let method = PostConnectSvc(inner);
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
    impl<T> Clone for IntegrationProviderServer<T> {
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
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.IntegrationProvider";
    impl<T> tonic::server::NamedService for IntegrationProviderServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod plugin_invoker_client {
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
    pub struct PluginInvokerClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl PluginInvokerClient<tonic::transport::Channel> {
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
    impl<T> PluginInvokerClient<T>
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
        ) -> PluginInvokerClient<InterceptedService<T, F>>
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
            PluginInvokerClient::new(InterceptedService::new(inner, interceptor))
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
        pub async fn exchange_invocation_token(
            &mut self,
            request: impl tonic::IntoRequest<super::ExchangeInvocationTokenRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ExchangeInvocationTokenResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.PluginInvoker/ExchangeInvocationToken",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.PluginInvoker",
                "ExchangeInvocationToken",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn invoke(
            &mut self,
            request: impl tonic::IntoRequest<super::PluginInvokeRequest>,
        ) -> std::result::Result<tonic::Response<super::OperationResult>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.PluginInvoker/Invoke");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.PluginInvoker",
                "Invoke",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn invoke_graph_ql(
            &mut self,
            request: impl tonic::IntoRequest<super::PluginInvokeGraphQlRequest>,
        ) -> std::result::Result<tonic::Response<super::OperationResult>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.PluginInvoker/InvokeGraphQL",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.PluginInvoker",
                "InvokeGraphQL",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod plugin_invoker_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with PluginInvokerServer.
    #[async_trait]
    pub trait PluginInvoker: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn exchange_invocation_token(
            &self,
            request: tonic::Request<super::ExchangeInvocationTokenRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ExchangeInvocationTokenResponse>,
            tonic::Status,
        >;
        ///
        async fn invoke(
            &self,
            request: tonic::Request<super::PluginInvokeRequest>,
        ) -> std::result::Result<tonic::Response<super::OperationResult>, tonic::Status>;
        ///
        async fn invoke_graph_ql(
            &self,
            request: tonic::Request<super::PluginInvokeGraphQlRequest>,
        ) -> std::result::Result<tonic::Response<super::OperationResult>, tonic::Status>;
    }
    ///
    #[derive(Debug)]
    pub struct PluginInvokerServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> PluginInvokerServer<T> {
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
    impl<T, B> tonic::codegen::Service<http::Request<B>> for PluginInvokerServer<T>
    where
        T: PluginInvoker,
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
                "/gestalt.provider.v1.PluginInvoker/ExchangeInvocationToken" => {
                    #[allow(non_camel_case_types)]
                    struct ExchangeInvocationTokenSvc<T: PluginInvoker>(pub Arc<T>);
                    impl<T: PluginInvoker>
                        tonic::server::UnaryService<super::ExchangeInvocationTokenRequest>
                        for ExchangeInvocationTokenSvc<T>
                    {
                        type Response = super::ExchangeInvocationTokenResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ExchangeInvocationTokenRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as PluginInvoker>::exchange_invocation_token(&inner, request)
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
                        let method = ExchangeInvocationTokenSvc(inner);
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
                "/gestalt.provider.v1.PluginInvoker/Invoke" => {
                    #[allow(non_camel_case_types)]
                    struct InvokeSvc<T: PluginInvoker>(pub Arc<T>);
                    impl<T: PluginInvoker> tonic::server::UnaryService<super::PluginInvokeRequest> for InvokeSvc<T> {
                        type Response = super::OperationResult;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::PluginInvokeRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as PluginInvoker>::invoke(&inner, request).await };
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
                "/gestalt.provider.v1.PluginInvoker/InvokeGraphQL" => {
                    #[allow(non_camel_case_types)]
                    struct InvokeGraphQLSvc<T: PluginInvoker>(pub Arc<T>);
                    impl<T: PluginInvoker>
                        tonic::server::UnaryService<super::PluginInvokeGraphQlRequest>
                        for InvokeGraphQLSvc<T>
                    {
                        type Response = super::OperationResult;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::PluginInvokeGraphQlRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as PluginInvoker>::invoke_graph_ql(&inner, request).await
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
    impl<T> Clone for PluginInvokerServer<T> {
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
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.PluginInvoker";
    impl<T> tonic::server::NamedService for PluginInvokerServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod agent_provider_client {
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
    pub struct AgentProviderClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl AgentProviderClient<tonic::transport::Channel> {
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
    impl<T> AgentProviderClient<T>
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
        ) -> AgentProviderClient<InterceptedService<T, F>>
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
            AgentProviderClient::new(InterceptedService::new(inner, interceptor))
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
        pub async fn create_session(
            &mut self,
            request: impl tonic::IntoRequest<super::CreateAgentProviderSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentSession>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentProvider/CreateSession",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentProvider",
                "CreateSession",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_session(
            &mut self,
            request: impl tonic::IntoRequest<super::GetAgentProviderSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentSession>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentProvider/GetSession",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentProvider",
                "GetSession",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
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
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentProvider/ListSessions",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentProvider",
                "ListSessions",
            ));
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
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentProvider/UpdateSession",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentProvider",
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
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentProvider/CreateTurn",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentProvider",
                "CreateTurn",
            ));
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
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.AgentProvider/GetTurn");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentProvider",
                "GetTurn",
            ));
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
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentProvider/ListTurns",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentProvider",
                "ListTurns",
            ));
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
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentProvider/CancelTurn",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentProvider",
                "CancelTurn",
            ));
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
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentProvider/ListTurnEvents",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentProvider",
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
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentProvider/GetInteraction",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentProvider",
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
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentProvider/ListInteractions",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentProvider",
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
                "/gestalt.provider.v1.AgentProvider/ResolveInteraction",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentProvider",
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
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentProvider/GetCapabilities",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentProvider",
                "GetCapabilities",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod agent_provider_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with AgentProviderServer.
    #[async_trait]
    pub trait AgentProvider: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn create_session(
            &self,
            request: tonic::Request<super::CreateAgentProviderSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentSession>, tonic::Status>;
        ///
        async fn get_session(
            &self,
            request: tonic::Request<super::GetAgentProviderSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentSession>, tonic::Status>;
        ///
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
    ///
    #[derive(Debug)]
    pub struct AgentProviderServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> AgentProviderServer<T> {
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
    impl<T, B> tonic::codegen::Service<http::Request<B>> for AgentProviderServer<T>
    where
        T: AgentProvider,
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
                "/gestalt.provider.v1.AgentProvider/CreateSession" => {
                    #[allow(non_camel_case_types)]
                    struct CreateSessionSvc<T: AgentProvider>(pub Arc<T>);
                    impl<T: AgentProvider>
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
                            let fut = async move {
                                <T as AgentProvider>::create_session(&inner, request).await
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
                "/gestalt.provider.v1.AgentProvider/GetSession" => {
                    #[allow(non_camel_case_types)]
                    struct GetSessionSvc<T: AgentProvider>(pub Arc<T>);
                    impl<T: AgentProvider>
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
                            let fut = async move {
                                <T as AgentProvider>::get_session(&inner, request).await
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
                "/gestalt.provider.v1.AgentProvider/ListSessions" => {
                    #[allow(non_camel_case_types)]
                    struct ListSessionsSvc<T: AgentProvider>(pub Arc<T>);
                    impl<T: AgentProvider>
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
                            let fut = async move {
                                <T as AgentProvider>::list_sessions(&inner, request).await
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
                "/gestalt.provider.v1.AgentProvider/UpdateSession" => {
                    #[allow(non_camel_case_types)]
                    struct UpdateSessionSvc<T: AgentProvider>(pub Arc<T>);
                    impl<T: AgentProvider>
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
                            let fut = async move {
                                <T as AgentProvider>::update_session(&inner, request).await
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
                "/gestalt.provider.v1.AgentProvider/CreateTurn" => {
                    #[allow(non_camel_case_types)]
                    struct CreateTurnSvc<T: AgentProvider>(pub Arc<T>);
                    impl<T: AgentProvider>
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
                            let fut = async move {
                                <T as AgentProvider>::create_turn(&inner, request).await
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
                "/gestalt.provider.v1.AgentProvider/GetTurn" => {
                    #[allow(non_camel_case_types)]
                    struct GetTurnSvc<T: AgentProvider>(pub Arc<T>);
                    impl<T: AgentProvider>
                        tonic::server::UnaryService<super::GetAgentProviderTurnRequest>
                        for GetTurnSvc<T>
                    {
                        type Response = super::AgentTurn;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetAgentProviderTurnRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AgentProvider>::get_turn(&inner, request).await
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
                "/gestalt.provider.v1.AgentProvider/ListTurns" => {
                    #[allow(non_camel_case_types)]
                    struct ListTurnsSvc<T: AgentProvider>(pub Arc<T>);
                    impl<T: AgentProvider>
                        tonic::server::UnaryService<super::ListAgentProviderTurnsRequest>
                        for ListTurnsSvc<T>
                    {
                        type Response = super::ListAgentProviderTurnsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListAgentProviderTurnsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AgentProvider>::list_turns(&inner, request).await
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
                "/gestalt.provider.v1.AgentProvider/CancelTurn" => {
                    #[allow(non_camel_case_types)]
                    struct CancelTurnSvc<T: AgentProvider>(pub Arc<T>);
                    impl<T: AgentProvider>
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
                            let fut = async move {
                                <T as AgentProvider>::cancel_turn(&inner, request).await
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
                "/gestalt.provider.v1.AgentProvider/ListTurnEvents" => {
                    #[allow(non_camel_case_types)]
                    struct ListTurnEventsSvc<T: AgentProvider>(pub Arc<T>);
                    impl<T: AgentProvider>
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
                                <T as AgentProvider>::list_turn_events(&inner, request).await
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
                "/gestalt.provider.v1.AgentProvider/GetInteraction" => {
                    #[allow(non_camel_case_types)]
                    struct GetInteractionSvc<T: AgentProvider>(pub Arc<T>);
                    impl<T: AgentProvider>
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
                            let fut = async move {
                                <T as AgentProvider>::get_interaction(&inner, request).await
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
                "/gestalt.provider.v1.AgentProvider/ListInteractions" => {
                    #[allow(non_camel_case_types)]
                    struct ListInteractionsSvc<T: AgentProvider>(pub Arc<T>);
                    impl<T: AgentProvider>
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
                                <T as AgentProvider>::list_interactions(&inner, request).await
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
                "/gestalt.provider.v1.AgentProvider/ResolveInteraction" => {
                    #[allow(non_camel_case_types)]
                    struct ResolveInteractionSvc<T: AgentProvider>(pub Arc<T>);
                    impl<T: AgentProvider>
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
                                <T as AgentProvider>::resolve_interaction(&inner, request).await
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
                "/gestalt.provider.v1.AgentProvider/GetCapabilities" => {
                    #[allow(non_camel_case_types)]
                    struct GetCapabilitiesSvc<T: AgentProvider>(pub Arc<T>);
                    impl<T: AgentProvider>
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
                                <T as AgentProvider>::get_capabilities(&inner, request).await
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
    impl<T> Clone for AgentProviderServer<T> {
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
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.AgentProvider";
    impl<T> tonic::server::NamedService for AgentProviderServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod agent_host_client {
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
    pub struct AgentHostClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl AgentHostClient<tonic::transport::Channel> {
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
    impl<T> AgentHostClient<T>
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
        ) -> AgentHostClient<InterceptedService<T, F>>
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
            AgentHostClient::new(InterceptedService::new(inner, interceptor))
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
        pub async fn search_tools(
            &mut self,
            request: impl tonic::IntoRequest<super::SearchAgentToolsRequest>,
        ) -> std::result::Result<tonic::Response<super::SearchAgentToolsResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.AgentHost/SearchTools");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentHost",
                "SearchTools",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_tools(
            &mut self,
            request: impl tonic::IntoRequest<super::ListAgentToolsRequest>,
        ) -> std::result::Result<tonic::Response<super::ListAgentToolsResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.AgentHost/ListTools");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentHost",
                "ListTools",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn execute_tool(
            &mut self,
            request: impl tonic::IntoRequest<super::ExecuteAgentToolRequest>,
        ) -> std::result::Result<tonic::Response<super::ExecuteAgentToolResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path =
                http::uri::PathAndQuery::from_static("/gestalt.provider.v1.AgentHost/ExecuteTool");
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentHost",
                "ExecuteTool",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod agent_host_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with AgentHostServer.
    #[async_trait]
    pub trait AgentHost: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn search_tools(
            &self,
            request: tonic::Request<super::SearchAgentToolsRequest>,
        ) -> std::result::Result<tonic::Response<super::SearchAgentToolsResponse>, tonic::Status>;
        ///
        async fn list_tools(
            &self,
            request: tonic::Request<super::ListAgentToolsRequest>,
        ) -> std::result::Result<tonic::Response<super::ListAgentToolsResponse>, tonic::Status>;
        ///
        async fn execute_tool(
            &self,
            request: tonic::Request<super::ExecuteAgentToolRequest>,
        ) -> std::result::Result<tonic::Response<super::ExecuteAgentToolResponse>, tonic::Status>;
    }
    ///
    #[derive(Debug)]
    pub struct AgentHostServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> AgentHostServer<T> {
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
    impl<T, B> tonic::codegen::Service<http::Request<B>> for AgentHostServer<T>
    where
        T: AgentHost,
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
                "/gestalt.provider.v1.AgentHost/SearchTools" => {
                    #[allow(non_camel_case_types)]
                    struct SearchToolsSvc<T: AgentHost>(pub Arc<T>);
                    impl<T: AgentHost> tonic::server::UnaryService<super::SearchAgentToolsRequest>
                        for SearchToolsSvc<T>
                    {
                        type Response = super::SearchAgentToolsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::SearchAgentToolsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AgentHost>::search_tools(&inner, request).await
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
                        let method = SearchToolsSvc(inner);
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
                "/gestalt.provider.v1.AgentHost/ListTools" => {
                    #[allow(non_camel_case_types)]
                    struct ListToolsSvc<T: AgentHost>(pub Arc<T>);
                    impl<T: AgentHost> tonic::server::UnaryService<super::ListAgentToolsRequest> for ListToolsSvc<T> {
                        type Response = super::ListAgentToolsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListAgentToolsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut =
                                async move { <T as AgentHost>::list_tools(&inner, request).await };
                            Box::pin(fut)
                        }
                    }
                    let accept_compression_encodings = self.accept_compression_encodings;
                    let send_compression_encodings = self.send_compression_encodings;
                    let max_decoding_message_size = self.max_decoding_message_size;
                    let max_encoding_message_size = self.max_encoding_message_size;
                    let inner = self.inner.clone();
                    let fut = async move {
                        let method = ListToolsSvc(inner);
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
                "/gestalt.provider.v1.AgentHost/ExecuteTool" => {
                    #[allow(non_camel_case_types)]
                    struct ExecuteToolSvc<T: AgentHost>(pub Arc<T>);
                    impl<T: AgentHost> tonic::server::UnaryService<super::ExecuteAgentToolRequest>
                        for ExecuteToolSvc<T>
                    {
                        type Response = super::ExecuteAgentToolResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ExecuteAgentToolRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AgentHost>::execute_tool(&inner, request).await
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
                        let method = ExecuteToolSvc(inner);
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
    impl<T> Clone for AgentHostServer<T> {
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
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.AgentHost";
    impl<T> tonic::server::NamedService for AgentHostServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod agent_manager_host_client {
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
    pub struct AgentManagerHostClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl AgentManagerHostClient<tonic::transport::Channel> {
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
    impl<T> AgentManagerHostClient<T>
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
        ) -> AgentManagerHostClient<InterceptedService<T, F>>
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
            AgentManagerHostClient::new(InterceptedService::new(inner, interceptor))
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
        pub async fn create_session(
            &mut self,
            request: impl tonic::IntoRequest<super::AgentManagerCreateSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentSession>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentManagerHost/CreateSession",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentManagerHost",
                "CreateSession",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_session(
            &mut self,
            request: impl tonic::IntoRequest<super::AgentManagerGetSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentSession>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentManagerHost/GetSession",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentManagerHost",
                "GetSession",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_sessions(
            &mut self,
            request: impl tonic::IntoRequest<super::AgentManagerListSessionsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::AgentManagerListSessionsResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentManagerHost/ListSessions",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentManagerHost",
                "ListSessions",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn update_session(
            &mut self,
            request: impl tonic::IntoRequest<super::AgentManagerUpdateSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentSession>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentManagerHost/UpdateSession",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentManagerHost",
                "UpdateSession",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn create_turn(
            &mut self,
            request: impl tonic::IntoRequest<super::AgentManagerCreateTurnRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentTurn>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentManagerHost/CreateTurn",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentManagerHost",
                "CreateTurn",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_turn(
            &mut self,
            request: impl tonic::IntoRequest<super::AgentManagerGetTurnRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentTurn>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentManagerHost/GetTurn",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentManagerHost",
                "GetTurn",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_turns(
            &mut self,
            request: impl tonic::IntoRequest<super::AgentManagerListTurnsRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentManagerListTurnsResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentManagerHost/ListTurns",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentManagerHost",
                "ListTurns",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn cancel_turn(
            &mut self,
            request: impl tonic::IntoRequest<super::AgentManagerCancelTurnRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentTurn>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentManagerHost/CancelTurn",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentManagerHost",
                "CancelTurn",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_turn_events(
            &mut self,
            request: impl tonic::IntoRequest<super::AgentManagerListTurnEventsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::AgentManagerListTurnEventsResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentManagerHost/ListTurnEvents",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentManagerHost",
                "ListTurnEvents",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_interactions(
            &mut self,
            request: impl tonic::IntoRequest<super::AgentManagerListInteractionsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::AgentManagerListInteractionsResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentManagerHost/ListInteractions",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentManagerHost",
                "ListInteractions",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn resolve_interaction(
            &mut self,
            request: impl tonic::IntoRequest<super::AgentManagerResolveInteractionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentInteraction>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AgentManagerHost/ResolveInteraction",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AgentManagerHost",
                "ResolveInteraction",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod agent_manager_host_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with AgentManagerHostServer.
    #[async_trait]
    pub trait AgentManagerHost: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn create_session(
            &self,
            request: tonic::Request<super::AgentManagerCreateSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentSession>, tonic::Status>;
        ///
        async fn get_session(
            &self,
            request: tonic::Request<super::AgentManagerGetSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentSession>, tonic::Status>;
        ///
        async fn list_sessions(
            &self,
            request: tonic::Request<super::AgentManagerListSessionsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::AgentManagerListSessionsResponse>,
            tonic::Status,
        >;
        ///
        async fn update_session(
            &self,
            request: tonic::Request<super::AgentManagerUpdateSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentSession>, tonic::Status>;
        ///
        async fn create_turn(
            &self,
            request: tonic::Request<super::AgentManagerCreateTurnRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentTurn>, tonic::Status>;
        ///
        async fn get_turn(
            &self,
            request: tonic::Request<super::AgentManagerGetTurnRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentTurn>, tonic::Status>;
        ///
        async fn list_turns(
            &self,
            request: tonic::Request<super::AgentManagerListTurnsRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentManagerListTurnsResponse>, tonic::Status>;
        ///
        async fn cancel_turn(
            &self,
            request: tonic::Request<super::AgentManagerCancelTurnRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentTurn>, tonic::Status>;
        ///
        async fn list_turn_events(
            &self,
            request: tonic::Request<super::AgentManagerListTurnEventsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::AgentManagerListTurnEventsResponse>,
            tonic::Status,
        >;
        ///
        async fn list_interactions(
            &self,
            request: tonic::Request<super::AgentManagerListInteractionsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::AgentManagerListInteractionsResponse>,
            tonic::Status,
        >;
        ///
        async fn resolve_interaction(
            &self,
            request: tonic::Request<super::AgentManagerResolveInteractionRequest>,
        ) -> std::result::Result<tonic::Response<super::AgentInteraction>, tonic::Status>;
    }
    ///
    #[derive(Debug)]
    pub struct AgentManagerHostServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> AgentManagerHostServer<T> {
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
    impl<T, B> tonic::codegen::Service<http::Request<B>> for AgentManagerHostServer<T>
    where
        T: AgentManagerHost,
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
                "/gestalt.provider.v1.AgentManagerHost/CreateSession" => {
                    #[allow(non_camel_case_types)]
                    struct CreateSessionSvc<T: AgentManagerHost>(pub Arc<T>);
                    impl<T: AgentManagerHost>
                        tonic::server::UnaryService<super::AgentManagerCreateSessionRequest>
                        for CreateSessionSvc<T>
                    {
                        type Response = super::AgentSession;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::AgentManagerCreateSessionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AgentManagerHost>::create_session(&inner, request).await
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
                "/gestalt.provider.v1.AgentManagerHost/GetSession" => {
                    #[allow(non_camel_case_types)]
                    struct GetSessionSvc<T: AgentManagerHost>(pub Arc<T>);
                    impl<T: AgentManagerHost>
                        tonic::server::UnaryService<super::AgentManagerGetSessionRequest>
                        for GetSessionSvc<T>
                    {
                        type Response = super::AgentSession;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::AgentManagerGetSessionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AgentManagerHost>::get_session(&inner, request).await
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
                "/gestalt.provider.v1.AgentManagerHost/ListSessions" => {
                    #[allow(non_camel_case_types)]
                    struct ListSessionsSvc<T: AgentManagerHost>(pub Arc<T>);
                    impl<T: AgentManagerHost>
                        tonic::server::UnaryService<super::AgentManagerListSessionsRequest>
                        for ListSessionsSvc<T>
                    {
                        type Response = super::AgentManagerListSessionsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::AgentManagerListSessionsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AgentManagerHost>::list_sessions(&inner, request).await
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
                "/gestalt.provider.v1.AgentManagerHost/UpdateSession" => {
                    #[allow(non_camel_case_types)]
                    struct UpdateSessionSvc<T: AgentManagerHost>(pub Arc<T>);
                    impl<T: AgentManagerHost>
                        tonic::server::UnaryService<super::AgentManagerUpdateSessionRequest>
                        for UpdateSessionSvc<T>
                    {
                        type Response = super::AgentSession;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::AgentManagerUpdateSessionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AgentManagerHost>::update_session(&inner, request).await
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
                "/gestalt.provider.v1.AgentManagerHost/CreateTurn" => {
                    #[allow(non_camel_case_types)]
                    struct CreateTurnSvc<T: AgentManagerHost>(pub Arc<T>);
                    impl<T: AgentManagerHost>
                        tonic::server::UnaryService<super::AgentManagerCreateTurnRequest>
                        for CreateTurnSvc<T>
                    {
                        type Response = super::AgentTurn;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::AgentManagerCreateTurnRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AgentManagerHost>::create_turn(&inner, request).await
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
                "/gestalt.provider.v1.AgentManagerHost/GetTurn" => {
                    #[allow(non_camel_case_types)]
                    struct GetTurnSvc<T: AgentManagerHost>(pub Arc<T>);
                    impl<T: AgentManagerHost>
                        tonic::server::UnaryService<super::AgentManagerGetTurnRequest>
                        for GetTurnSvc<T>
                    {
                        type Response = super::AgentTurn;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::AgentManagerGetTurnRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AgentManagerHost>::get_turn(&inner, request).await
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
                "/gestalt.provider.v1.AgentManagerHost/ListTurns" => {
                    #[allow(non_camel_case_types)]
                    struct ListTurnsSvc<T: AgentManagerHost>(pub Arc<T>);
                    impl<T: AgentManagerHost>
                        tonic::server::UnaryService<super::AgentManagerListTurnsRequest>
                        for ListTurnsSvc<T>
                    {
                        type Response = super::AgentManagerListTurnsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::AgentManagerListTurnsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AgentManagerHost>::list_turns(&inner, request).await
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
                "/gestalt.provider.v1.AgentManagerHost/CancelTurn" => {
                    #[allow(non_camel_case_types)]
                    struct CancelTurnSvc<T: AgentManagerHost>(pub Arc<T>);
                    impl<T: AgentManagerHost>
                        tonic::server::UnaryService<super::AgentManagerCancelTurnRequest>
                        for CancelTurnSvc<T>
                    {
                        type Response = super::AgentTurn;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::AgentManagerCancelTurnRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AgentManagerHost>::cancel_turn(&inner, request).await
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
                "/gestalt.provider.v1.AgentManagerHost/ListTurnEvents" => {
                    #[allow(non_camel_case_types)]
                    struct ListTurnEventsSvc<T: AgentManagerHost>(pub Arc<T>);
                    impl<T: AgentManagerHost>
                        tonic::server::UnaryService<super::AgentManagerListTurnEventsRequest>
                        for ListTurnEventsSvc<T>
                    {
                        type Response = super::AgentManagerListTurnEventsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::AgentManagerListTurnEventsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AgentManagerHost>::list_turn_events(&inner, request).await
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
                "/gestalt.provider.v1.AgentManagerHost/ListInteractions" => {
                    #[allow(non_camel_case_types)]
                    struct ListInteractionsSvc<T: AgentManagerHost>(pub Arc<T>);
                    impl<T: AgentManagerHost>
                        tonic::server::UnaryService<super::AgentManagerListInteractionsRequest>
                        for ListInteractionsSvc<T>
                    {
                        type Response = super::AgentManagerListInteractionsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::AgentManagerListInteractionsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AgentManagerHost>::list_interactions(&inner, request).await
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
                "/gestalt.provider.v1.AgentManagerHost/ResolveInteraction" => {
                    #[allow(non_camel_case_types)]
                    struct ResolveInteractionSvc<T: AgentManagerHost>(pub Arc<T>);
                    impl<T: AgentManagerHost>
                        tonic::server::UnaryService<super::AgentManagerResolveInteractionRequest>
                        for ResolveInteractionSvc<T>
                    {
                        type Response = super::AgentInteraction;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::AgentManagerResolveInteractionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AgentManagerHost>::resolve_interaction(&inner, request).await
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
    impl<T> Clone for AgentManagerHostServer<T> {
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
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.AgentManagerHost";
    impl<T> tonic::server::NamedService for AgentManagerHostServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod authentication_provider_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    /** AuthenticationProvider models the shared Gestalt authentication-provider
     protocol.
    */
    #[derive(Debug, Clone)]
    pub struct AuthenticationProviderClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl AuthenticationProviderClient<tonic::transport::Channel> {
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
    impl<T> AuthenticationProviderClient<T>
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
        ) -> AuthenticationProviderClient<InterceptedService<T, F>>
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
            AuthenticationProviderClient::new(InterceptedService::new(inner, interceptor))
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
        pub async fn begin_login(
            &mut self,
            request: impl tonic::IntoRequest<super::BeginLoginRequest>,
        ) -> std::result::Result<tonic::Response<super::BeginLoginResponse>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AuthenticationProvider/BeginLogin",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AuthenticationProvider",
                "BeginLogin",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn complete_login(
            &mut self,
            request: impl tonic::IntoRequest<super::CompleteLoginRequest>,
        ) -> std::result::Result<tonic::Response<super::AuthenticatedUser>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AuthenticationProvider/CompleteLogin",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AuthenticationProvider",
                "CompleteLogin",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn validate_external_token(
            &mut self,
            request: impl tonic::IntoRequest<super::ValidateExternalTokenRequest>,
        ) -> std::result::Result<tonic::Response<super::AuthenticatedUser>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AuthenticationProvider/ValidateExternalToken",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AuthenticationProvider",
                "ValidateExternalToken",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_session_settings(
            &mut self,
            request: impl tonic::IntoRequest<()>,
        ) -> std::result::Result<tonic::Response<super::AuthSessionSettings>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.AuthenticationProvider/GetSessionSettings",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.AuthenticationProvider",
                "GetSessionSettings",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod authentication_provider_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with AuthenticationProviderServer.
    #[async_trait]
    pub trait AuthenticationProvider: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn begin_login(
            &self,
            request: tonic::Request<super::BeginLoginRequest>,
        ) -> std::result::Result<tonic::Response<super::BeginLoginResponse>, tonic::Status>;
        ///
        async fn complete_login(
            &self,
            request: tonic::Request<super::CompleteLoginRequest>,
        ) -> std::result::Result<tonic::Response<super::AuthenticatedUser>, tonic::Status>;
        ///
        async fn validate_external_token(
            &self,
            request: tonic::Request<super::ValidateExternalTokenRequest>,
        ) -> std::result::Result<tonic::Response<super::AuthenticatedUser>, tonic::Status>;
        ///
        async fn get_session_settings(
            &self,
            request: tonic::Request<()>,
        ) -> std::result::Result<tonic::Response<super::AuthSessionSettings>, tonic::Status>;
    }
    /** AuthenticationProvider models the shared Gestalt authentication-provider
     protocol.
    */
    #[derive(Debug)]
    pub struct AuthenticationProviderServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> AuthenticationProviderServer<T> {
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
    impl<T, B> tonic::codegen::Service<http::Request<B>> for AuthenticationProviderServer<T>
    where
        T: AuthenticationProvider,
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
                "/gestalt.provider.v1.AuthenticationProvider/BeginLogin" => {
                    #[allow(non_camel_case_types)]
                    struct BeginLoginSvc<T: AuthenticationProvider>(pub Arc<T>);
                    impl<T: AuthenticationProvider>
                        tonic::server::UnaryService<super::BeginLoginRequest> for BeginLoginSvc<T>
                    {
                        type Response = super::BeginLoginResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::BeginLoginRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AuthenticationProvider>::begin_login(&inner, request).await
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
                        let method = BeginLoginSvc(inner);
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
                "/gestalt.provider.v1.AuthenticationProvider/CompleteLogin" => {
                    #[allow(non_camel_case_types)]
                    struct CompleteLoginSvc<T: AuthenticationProvider>(pub Arc<T>);
                    impl<T: AuthenticationProvider>
                        tonic::server::UnaryService<super::CompleteLoginRequest>
                        for CompleteLoginSvc<T>
                    {
                        type Response = super::AuthenticatedUser;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CompleteLoginRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AuthenticationProvider>::complete_login(&inner, request).await
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
                        let method = CompleteLoginSvc(inner);
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
                "/gestalt.provider.v1.AuthenticationProvider/ValidateExternalToken" => {
                    #[allow(non_camel_case_types)]
                    struct ValidateExternalTokenSvc<T: AuthenticationProvider>(pub Arc<T>);
                    impl<T: AuthenticationProvider>
                        tonic::server::UnaryService<super::ValidateExternalTokenRequest>
                        for ValidateExternalTokenSvc<T>
                    {
                        type Response = super::AuthenticatedUser;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ValidateExternalTokenRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AuthenticationProvider>::validate_external_token(
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
                        let method = ValidateExternalTokenSvc(inner);
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
                "/gestalt.provider.v1.AuthenticationProvider/GetSessionSettings" => {
                    #[allow(non_camel_case_types)]
                    struct GetSessionSettingsSvc<T: AuthenticationProvider>(pub Arc<T>);
                    impl<T: AuthenticationProvider> tonic::server::UnaryService<()> for GetSessionSettingsSvc<T> {
                        type Response = super::AuthSessionSettings;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(&mut self, request: tonic::Request<()>) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as AuthenticationProvider>::get_session_settings(&inner, request)
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
                        let method = GetSessionSettingsSvc(inner);
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
    impl<T> Clone for AuthenticationProviderServer<T> {
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
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.AuthenticationProvider";
    impl<T> tonic::server::NamedService for AuthenticationProviderServer<T> {
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
        ///
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
        ///
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
        /** Lifecycle
        */
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
        /** Bulk operations (with optional key range)
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
        /** Lifecycle
        */
        async fn create_object_store(
            &self,
            request: tonic::Request<super::CreateObjectStoreRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn delete_object_store(
            &self,
            request: tonic::Request<super::DeleteObjectStoreRequest>,
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
        /** Bulk operations (with optional key range)
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
pub mod plugin_runtime_log_host_client {
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
    pub struct PluginRuntimeLogHostClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl PluginRuntimeLogHostClient<tonic::transport::Channel> {
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
    impl<T> PluginRuntimeLogHostClient<T>
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
        ) -> PluginRuntimeLogHostClient<InterceptedService<T, F>>
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
            PluginRuntimeLogHostClient::new(InterceptedService::new(inner, interceptor))
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
        pub async fn append_logs(
            &mut self,
            request: impl tonic::IntoRequest<super::AppendPluginRuntimeLogsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::AppendPluginRuntimeLogsResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.PluginRuntimeLogHost/AppendLogs",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.PluginRuntimeLogHost",
                "AppendLogs",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod plugin_runtime_log_host_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with PluginRuntimeLogHostServer.
    #[async_trait]
    pub trait PluginRuntimeLogHost: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn append_logs(
            &self,
            request: tonic::Request<super::AppendPluginRuntimeLogsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::AppendPluginRuntimeLogsResponse>,
            tonic::Status,
        >;
    }
    ///
    #[derive(Debug)]
    pub struct PluginRuntimeLogHostServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> PluginRuntimeLogHostServer<T> {
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
    impl<T, B> tonic::codegen::Service<http::Request<B>> for PluginRuntimeLogHostServer<T>
    where
        T: PluginRuntimeLogHost,
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
                "/gestalt.provider.v1.PluginRuntimeLogHost/AppendLogs" => {
                    #[allow(non_camel_case_types)]
                    struct AppendLogsSvc<T: PluginRuntimeLogHost>(pub Arc<T>);
                    impl<T: PluginRuntimeLogHost>
                        tonic::server::UnaryService<super::AppendPluginRuntimeLogsRequest>
                        for AppendLogsSvc<T>
                    {
                        type Response = super::AppendPluginRuntimeLogsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::AppendPluginRuntimeLogsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as PluginRuntimeLogHost>::append_logs(&inner, request).await
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
                        let method = AppendLogsSvc(inner);
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
    impl<T> Clone for PluginRuntimeLogHostServer<T> {
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
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.PluginRuntimeLogHost";
    impl<T> tonic::server::NamedService for PluginRuntimeLogHostServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod plugin_runtime_provider_client {
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
    pub struct PluginRuntimeProviderClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl PluginRuntimeProviderClient<tonic::transport::Channel> {
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
    impl<T> PluginRuntimeProviderClient<T>
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
        ) -> PluginRuntimeProviderClient<InterceptedService<T, F>>
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
            PluginRuntimeProviderClient::new(InterceptedService::new(inner, interceptor))
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
        ) -> std::result::Result<tonic::Response<super::PluginRuntimeSupport>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.PluginRuntimeProvider/GetSupport",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.PluginRuntimeProvider",
                "GetSupport",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn start_session(
            &mut self,
            request: impl tonic::IntoRequest<super::StartPluginRuntimeSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::PluginRuntimeSession>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.PluginRuntimeProvider/StartSession",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.PluginRuntimeProvider",
                "StartSession",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_session(
            &mut self,
            request: impl tonic::IntoRequest<super::GetPluginRuntimeSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::PluginRuntimeSession>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.PluginRuntimeProvider/GetSession",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.PluginRuntimeProvider",
                "GetSession",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_sessions(
            &mut self,
            request: impl tonic::IntoRequest<super::ListPluginRuntimeSessionsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListPluginRuntimeSessionsResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.PluginRuntimeProvider/ListSessions",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.PluginRuntimeProvider",
                "ListSessions",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn stop_session(
            &mut self,
            request: impl tonic::IntoRequest<super::StopPluginRuntimeSessionRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.PluginRuntimeProvider/StopSession",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.PluginRuntimeProvider",
                "StopSession",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn start_plugin(
            &mut self,
            request: impl tonic::IntoRequest<super::StartHostedPluginRequest>,
        ) -> std::result::Result<tonic::Response<super::HostedPlugin>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.PluginRuntimeProvider/StartPlugin",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.PluginRuntimeProvider",
                "StartPlugin",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod plugin_runtime_provider_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with PluginRuntimeProviderServer.
    #[async_trait]
    pub trait PluginRuntimeProvider: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn get_support(
            &self,
            request: tonic::Request<()>,
        ) -> std::result::Result<tonic::Response<super::PluginRuntimeSupport>, tonic::Status>;
        ///
        async fn start_session(
            &self,
            request: tonic::Request<super::StartPluginRuntimeSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::PluginRuntimeSession>, tonic::Status>;
        ///
        async fn get_session(
            &self,
            request: tonic::Request<super::GetPluginRuntimeSessionRequest>,
        ) -> std::result::Result<tonic::Response<super::PluginRuntimeSession>, tonic::Status>;
        ///
        async fn list_sessions(
            &self,
            request: tonic::Request<super::ListPluginRuntimeSessionsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListPluginRuntimeSessionsResponse>,
            tonic::Status,
        >;
        ///
        async fn stop_session(
            &self,
            request: tonic::Request<super::StopPluginRuntimeSessionRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn start_plugin(
            &self,
            request: tonic::Request<super::StartHostedPluginRequest>,
        ) -> std::result::Result<tonic::Response<super::HostedPlugin>, tonic::Status>;
    }
    ///
    #[derive(Debug)]
    pub struct PluginRuntimeProviderServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> PluginRuntimeProviderServer<T> {
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
    impl<T, B> tonic::codegen::Service<http::Request<B>> for PluginRuntimeProviderServer<T>
    where
        T: PluginRuntimeProvider,
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
                "/gestalt.provider.v1.PluginRuntimeProvider/GetSupport" => {
                    #[allow(non_camel_case_types)]
                    struct GetSupportSvc<T: PluginRuntimeProvider>(pub Arc<T>);
                    impl<T: PluginRuntimeProvider> tonic::server::UnaryService<()> for GetSupportSvc<T> {
                        type Response = super::PluginRuntimeSupport;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(&mut self, request: tonic::Request<()>) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as PluginRuntimeProvider>::get_support(&inner, request).await
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
                "/gestalt.provider.v1.PluginRuntimeProvider/StartSession" => {
                    #[allow(non_camel_case_types)]
                    struct StartSessionSvc<T: PluginRuntimeProvider>(pub Arc<T>);
                    impl<T: PluginRuntimeProvider>
                        tonic::server::UnaryService<super::StartPluginRuntimeSessionRequest>
                        for StartSessionSvc<T>
                    {
                        type Response = super::PluginRuntimeSession;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::StartPluginRuntimeSessionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as PluginRuntimeProvider>::start_session(&inner, request).await
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
                "/gestalt.provider.v1.PluginRuntimeProvider/GetSession" => {
                    #[allow(non_camel_case_types)]
                    struct GetSessionSvc<T: PluginRuntimeProvider>(pub Arc<T>);
                    impl<T: PluginRuntimeProvider>
                        tonic::server::UnaryService<super::GetPluginRuntimeSessionRequest>
                        for GetSessionSvc<T>
                    {
                        type Response = super::PluginRuntimeSession;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetPluginRuntimeSessionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as PluginRuntimeProvider>::get_session(&inner, request).await
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
                "/gestalt.provider.v1.PluginRuntimeProvider/ListSessions" => {
                    #[allow(non_camel_case_types)]
                    struct ListSessionsSvc<T: PluginRuntimeProvider>(pub Arc<T>);
                    impl<T: PluginRuntimeProvider>
                        tonic::server::UnaryService<super::ListPluginRuntimeSessionsRequest>
                        for ListSessionsSvc<T>
                    {
                        type Response = super::ListPluginRuntimeSessionsResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListPluginRuntimeSessionsRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as PluginRuntimeProvider>::list_sessions(&inner, request).await
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
                "/gestalt.provider.v1.PluginRuntimeProvider/StopSession" => {
                    #[allow(non_camel_case_types)]
                    struct StopSessionSvc<T: PluginRuntimeProvider>(pub Arc<T>);
                    impl<T: PluginRuntimeProvider>
                        tonic::server::UnaryService<super::StopPluginRuntimeSessionRequest>
                        for StopSessionSvc<T>
                    {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::StopPluginRuntimeSessionRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as PluginRuntimeProvider>::stop_session(&inner, request).await
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
                "/gestalt.provider.v1.PluginRuntimeProvider/StartPlugin" => {
                    #[allow(non_camel_case_types)]
                    struct StartPluginSvc<T: PluginRuntimeProvider>(pub Arc<T>);
                    impl<T: PluginRuntimeProvider>
                        tonic::server::UnaryService<super::StartHostedPluginRequest>
                        for StartPluginSvc<T>
                    {
                        type Response = super::HostedPlugin;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::StartHostedPluginRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as PluginRuntimeProvider>::start_plugin(&inner, request).await
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
                        let method = StartPluginSvc(inner);
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
    impl<T> Clone for PluginRuntimeProviderServer<T> {
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
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.PluginRuntimeProvider";
    impl<T> tonic::server::NamedService for PluginRuntimeProviderServer<T> {
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
        ///
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
        ///
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
     bindings. It is registered by gestaltd for plugins and is not implemented by
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
     bindings. It is registered by gestaltd for plugins and is not implemented by
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
pub mod secrets_provider_client {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::http::Uri;
    use tonic::codegen::*;
    /** SecretsProvider models the shared Gestalt secrets-provider protocol.
    */
    #[derive(Debug, Clone)]
    pub struct SecretsProviderClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl SecretsProviderClient<tonic::transport::Channel> {
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
    impl<T> SecretsProviderClient<T>
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
        ) -> SecretsProviderClient<InterceptedService<T, F>>
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
            SecretsProviderClient::new(InterceptedService::new(inner, interceptor))
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
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.SecretsProvider/GetSecret",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.SecretsProvider",
                "GetSecret",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod secrets_provider_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with SecretsProviderServer.
    #[async_trait]
    pub trait SecretsProvider: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn get_secret(
            &self,
            request: tonic::Request<super::GetSecretRequest>,
        ) -> std::result::Result<tonic::Response<super::GetSecretResponse>, tonic::Status>;
    }
    /** SecretsProvider models the shared Gestalt secrets-provider protocol.
    */
    #[derive(Debug)]
    pub struct SecretsProviderServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> SecretsProviderServer<T> {
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
    impl<T, B> tonic::codegen::Service<http::Request<B>> for SecretsProviderServer<T>
    where
        T: SecretsProvider,
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
                "/gestalt.provider.v1.SecretsProvider/GetSecret" => {
                    #[allow(non_camel_case_types)]
                    struct GetSecretSvc<T: SecretsProvider>(pub Arc<T>);
                    impl<T: SecretsProvider> tonic::server::UnaryService<super::GetSecretRequest> for GetSecretSvc<T> {
                        type Response = super::GetSecretResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetSecretRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as SecretsProvider>::get_secret(&inner, request).await
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
    impl<T> Clone for SecretsProviderServer<T> {
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
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.SecretsProvider";
    impl<T> tonic::server::NamedService for SecretsProviderServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod workflow_provider_client {
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
    pub struct WorkflowProviderClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl WorkflowProviderClient<tonic::transport::Channel> {
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
    impl<T> WorkflowProviderClient<T>
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
        ) -> WorkflowProviderClient<InterceptedService<T, F>>
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
            WorkflowProviderClient::new(InterceptedService::new(inner, interceptor))
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
        pub async fn start_run(
            &mut self,
            request: impl tonic::IntoRequest<super::StartWorkflowProviderRunRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowRun>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/StartRun",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "StartRun",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_run(
            &mut self,
            request: impl tonic::IntoRequest<super::GetWorkflowProviderRunRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowRun>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/GetRun",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "GetRun",
            ));
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
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/ListRuns",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "ListRuns",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn cancel_run(
            &mut self,
            request: impl tonic::IntoRequest<super::CancelWorkflowProviderRunRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowRun>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/CancelRun",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "CancelRun",
            ));
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
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/SignalRun",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "SignalRun",
            ));
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
                "/gestalt.provider.v1.WorkflowProvider/SignalOrStartRun",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "SignalOrStartRun",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn upsert_schedule(
            &mut self,
            request: impl tonic::IntoRequest<super::UpsertWorkflowProviderScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowSchedule>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/UpsertSchedule",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "UpsertSchedule",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_schedule(
            &mut self,
            request: impl tonic::IntoRequest<super::GetWorkflowProviderScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowSchedule>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/GetSchedule",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "GetSchedule",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_schedules(
            &mut self,
            request: impl tonic::IntoRequest<super::ListWorkflowProviderSchedulesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListWorkflowProviderSchedulesResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/ListSchedules",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "ListSchedules",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn delete_schedule(
            &mut self,
            request: impl tonic::IntoRequest<super::DeleteWorkflowProviderScheduleRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/DeleteSchedule",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "DeleteSchedule",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn pause_schedule(
            &mut self,
            request: impl tonic::IntoRequest<super::PauseWorkflowProviderScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowSchedule>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/PauseSchedule",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "PauseSchedule",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn resume_schedule(
            &mut self,
            request: impl tonic::IntoRequest<super::ResumeWorkflowProviderScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowSchedule>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/ResumeSchedule",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "ResumeSchedule",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn upsert_event_trigger(
            &mut self,
            request: impl tonic::IntoRequest<super::UpsertWorkflowProviderEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowEventTrigger>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/UpsertEventTrigger",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "UpsertEventTrigger",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_event_trigger(
            &mut self,
            request: impl tonic::IntoRequest<super::GetWorkflowProviderEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowEventTrigger>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/GetEventTrigger",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "GetEventTrigger",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_event_triggers(
            &mut self,
            request: impl tonic::IntoRequest<super::ListWorkflowProviderEventTriggersRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListWorkflowProviderEventTriggersResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/ListEventTriggers",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "ListEventTriggers",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn delete_event_trigger(
            &mut self,
            request: impl tonic::IntoRequest<super::DeleteWorkflowProviderEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/DeleteEventTrigger",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "DeleteEventTrigger",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn pause_event_trigger(
            &mut self,
            request: impl tonic::IntoRequest<super::PauseWorkflowProviderEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowEventTrigger>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/PauseEventTrigger",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "PauseEventTrigger",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn resume_event_trigger(
            &mut self,
            request: impl tonic::IntoRequest<super::ResumeWorkflowProviderEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowEventTrigger>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/ResumeEventTrigger",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "ResumeEventTrigger",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn put_execution_reference(
            &mut self,
            request: impl tonic::IntoRequest<super::PutWorkflowExecutionReferenceRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowExecutionReference>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/PutExecutionReference",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "PutExecutionReference",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_execution_reference(
            &mut self,
            request: impl tonic::IntoRequest<super::GetWorkflowExecutionReferenceRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowExecutionReference>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/GetExecutionReference",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "GetExecutionReference",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn list_execution_references(
            &mut self,
            request: impl tonic::IntoRequest<super::ListWorkflowExecutionReferencesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListWorkflowExecutionReferencesResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/ListExecutionReferences",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "ListExecutionReferences",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn publish_event(
            &mut self,
            request: impl tonic::IntoRequest<super::PublishWorkflowProviderEventRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowProvider/PublishEvent",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowProvider",
                "PublishEvent",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod workflow_provider_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with WorkflowProviderServer.
    #[async_trait]
    pub trait WorkflowProvider: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn start_run(
            &self,
            request: tonic::Request<super::StartWorkflowProviderRunRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowRun>, tonic::Status>;
        ///
        async fn get_run(
            &self,
            request: tonic::Request<super::GetWorkflowProviderRunRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowRun>, tonic::Status>;
        ///
        async fn list_runs(
            &self,
            request: tonic::Request<super::ListWorkflowProviderRunsRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListWorkflowProviderRunsResponse>,
            tonic::Status,
        >;
        ///
        async fn cancel_run(
            &self,
            request: tonic::Request<super::CancelWorkflowProviderRunRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowRun>, tonic::Status>;
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
        async fn upsert_schedule(
            &self,
            request: tonic::Request<super::UpsertWorkflowProviderScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowSchedule>, tonic::Status>;
        ///
        async fn get_schedule(
            &self,
            request: tonic::Request<super::GetWorkflowProviderScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowSchedule>, tonic::Status>;
        ///
        async fn list_schedules(
            &self,
            request: tonic::Request<super::ListWorkflowProviderSchedulesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListWorkflowProviderSchedulesResponse>,
            tonic::Status,
        >;
        ///
        async fn delete_schedule(
            &self,
            request: tonic::Request<super::DeleteWorkflowProviderScheduleRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn pause_schedule(
            &self,
            request: tonic::Request<super::PauseWorkflowProviderScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowSchedule>, tonic::Status>;
        ///
        async fn resume_schedule(
            &self,
            request: tonic::Request<super::ResumeWorkflowProviderScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowSchedule>, tonic::Status>;
        ///
        async fn upsert_event_trigger(
            &self,
            request: tonic::Request<super::UpsertWorkflowProviderEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowEventTrigger>, tonic::Status>;
        ///
        async fn get_event_trigger(
            &self,
            request: tonic::Request<super::GetWorkflowProviderEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowEventTrigger>, tonic::Status>;
        ///
        async fn list_event_triggers(
            &self,
            request: tonic::Request<super::ListWorkflowProviderEventTriggersRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListWorkflowProviderEventTriggersResponse>,
            tonic::Status,
        >;
        ///
        async fn delete_event_trigger(
            &self,
            request: tonic::Request<super::DeleteWorkflowProviderEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn pause_event_trigger(
            &self,
            request: tonic::Request<super::PauseWorkflowProviderEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowEventTrigger>, tonic::Status>;
        ///
        async fn resume_event_trigger(
            &self,
            request: tonic::Request<super::ResumeWorkflowProviderEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::BoundWorkflowEventTrigger>, tonic::Status>;
        ///
        async fn put_execution_reference(
            &self,
            request: tonic::Request<super::PutWorkflowExecutionReferenceRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowExecutionReference>, tonic::Status>;
        ///
        async fn get_execution_reference(
            &self,
            request: tonic::Request<super::GetWorkflowExecutionReferenceRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowExecutionReference>, tonic::Status>;
        ///
        async fn list_execution_references(
            &self,
            request: tonic::Request<super::ListWorkflowExecutionReferencesRequest>,
        ) -> std::result::Result<
            tonic::Response<super::ListWorkflowExecutionReferencesResponse>,
            tonic::Status,
        >;
        ///
        async fn publish_event(
            &self,
            request: tonic::Request<super::PublishWorkflowProviderEventRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
    }
    ///
    #[derive(Debug)]
    pub struct WorkflowProviderServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> WorkflowProviderServer<T> {
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
    impl<T, B> tonic::codegen::Service<http::Request<B>> for WorkflowProviderServer<T>
    where
        T: WorkflowProvider,
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
                "/gestalt.provider.v1.WorkflowProvider/StartRun" => {
                    #[allow(non_camel_case_types)]
                    struct StartRunSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<super::StartWorkflowProviderRunRequest>
                        for StartRunSvc<T>
                    {
                        type Response = super::BoundWorkflowRun;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::StartWorkflowProviderRunRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::start_run(&inner, request).await
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
                "/gestalt.provider.v1.WorkflowProvider/GetRun" => {
                    #[allow(non_camel_case_types)]
                    struct GetRunSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<super::GetWorkflowProviderRunRequest>
                        for GetRunSvc<T>
                    {
                        type Response = super::BoundWorkflowRun;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetWorkflowProviderRunRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::get_run(&inner, request).await
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
                "/gestalt.provider.v1.WorkflowProvider/ListRuns" => {
                    #[allow(non_camel_case_types)]
                    struct ListRunsSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
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
                            let fut = async move {
                                <T as WorkflowProvider>::list_runs(&inner, request).await
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
                "/gestalt.provider.v1.WorkflowProvider/CancelRun" => {
                    #[allow(non_camel_case_types)]
                    struct CancelRunSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<super::CancelWorkflowProviderRunRequest>
                        for CancelRunSvc<T>
                    {
                        type Response = super::BoundWorkflowRun;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::CancelWorkflowProviderRunRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::cancel_run(&inner, request).await
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
                "/gestalt.provider.v1.WorkflowProvider/SignalRun" => {
                    #[allow(non_camel_case_types)]
                    struct SignalRunSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
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
                            let fut = async move {
                                <T as WorkflowProvider>::signal_run(&inner, request).await
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
                "/gestalt.provider.v1.WorkflowProvider/SignalOrStartRun" => {
                    #[allow(non_camel_case_types)]
                    struct SignalOrStartRunSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
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
                                <T as WorkflowProvider>::signal_or_start_run(&inner, request).await
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
                "/gestalt.provider.v1.WorkflowProvider/UpsertSchedule" => {
                    #[allow(non_camel_case_types)]
                    struct UpsertScheduleSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<super::UpsertWorkflowProviderScheduleRequest>
                        for UpsertScheduleSvc<T>
                    {
                        type Response = super::BoundWorkflowSchedule;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::UpsertWorkflowProviderScheduleRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::upsert_schedule(&inner, request).await
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
                        let method = UpsertScheduleSvc(inner);
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
                "/gestalt.provider.v1.WorkflowProvider/GetSchedule" => {
                    #[allow(non_camel_case_types)]
                    struct GetScheduleSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<super::GetWorkflowProviderScheduleRequest>
                        for GetScheduleSvc<T>
                    {
                        type Response = super::BoundWorkflowSchedule;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetWorkflowProviderScheduleRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::get_schedule(&inner, request).await
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
                        let method = GetScheduleSvc(inner);
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
                "/gestalt.provider.v1.WorkflowProvider/ListSchedules" => {
                    #[allow(non_camel_case_types)]
                    struct ListSchedulesSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<super::ListWorkflowProviderSchedulesRequest>
                        for ListSchedulesSvc<T>
                    {
                        type Response = super::ListWorkflowProviderSchedulesResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListWorkflowProviderSchedulesRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::list_schedules(&inner, request).await
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
                        let method = ListSchedulesSvc(inner);
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
                "/gestalt.provider.v1.WorkflowProvider/DeleteSchedule" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteScheduleSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<super::DeleteWorkflowProviderScheduleRequest>
                        for DeleteScheduleSvc<T>
                    {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::DeleteWorkflowProviderScheduleRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::delete_schedule(&inner, request).await
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
                        let method = DeleteScheduleSvc(inner);
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
                "/gestalt.provider.v1.WorkflowProvider/PauseSchedule" => {
                    #[allow(non_camel_case_types)]
                    struct PauseScheduleSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<super::PauseWorkflowProviderScheduleRequest>
                        for PauseScheduleSvc<T>
                    {
                        type Response = super::BoundWorkflowSchedule;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::PauseWorkflowProviderScheduleRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::pause_schedule(&inner, request).await
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
                        let method = PauseScheduleSvc(inner);
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
                "/gestalt.provider.v1.WorkflowProvider/ResumeSchedule" => {
                    #[allow(non_camel_case_types)]
                    struct ResumeScheduleSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<super::ResumeWorkflowProviderScheduleRequest>
                        for ResumeScheduleSvc<T>
                    {
                        type Response = super::BoundWorkflowSchedule;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ResumeWorkflowProviderScheduleRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::resume_schedule(&inner, request).await
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
                        let method = ResumeScheduleSvc(inner);
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
                "/gestalt.provider.v1.WorkflowProvider/UpsertEventTrigger" => {
                    #[allow(non_camel_case_types)]
                    struct UpsertEventTriggerSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<
                            super::UpsertWorkflowProviderEventTriggerRequest,
                        > for UpsertEventTriggerSvc<T>
                    {
                        type Response = super::BoundWorkflowEventTrigger;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::UpsertWorkflowProviderEventTriggerRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::upsert_event_trigger(&inner, request).await
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
                        let method = UpsertEventTriggerSvc(inner);
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
                "/gestalt.provider.v1.WorkflowProvider/GetEventTrigger" => {
                    #[allow(non_camel_case_types)]
                    struct GetEventTriggerSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<super::GetWorkflowProviderEventTriggerRequest>
                        for GetEventTriggerSvc<T>
                    {
                        type Response = super::BoundWorkflowEventTrigger;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetWorkflowProviderEventTriggerRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::get_event_trigger(&inner, request).await
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
                        let method = GetEventTriggerSvc(inner);
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
                "/gestalt.provider.v1.WorkflowProvider/ListEventTriggers" => {
                    #[allow(non_camel_case_types)]
                    struct ListEventTriggersSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<super::ListWorkflowProviderEventTriggersRequest>
                        for ListEventTriggersSvc<T>
                    {
                        type Response = super::ListWorkflowProviderEventTriggersResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::ListWorkflowProviderEventTriggersRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::list_event_triggers(&inner, request).await
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
                        let method = ListEventTriggersSvc(inner);
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
                "/gestalt.provider.v1.WorkflowProvider/DeleteEventTrigger" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteEventTriggerSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<
                            super::DeleteWorkflowProviderEventTriggerRequest,
                        > for DeleteEventTriggerSvc<T>
                    {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::DeleteWorkflowProviderEventTriggerRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::delete_event_trigger(&inner, request).await
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
                        let method = DeleteEventTriggerSvc(inner);
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
                "/gestalt.provider.v1.WorkflowProvider/PauseEventTrigger" => {
                    #[allow(non_camel_case_types)]
                    struct PauseEventTriggerSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<super::PauseWorkflowProviderEventTriggerRequest>
                        for PauseEventTriggerSvc<T>
                    {
                        type Response = super::BoundWorkflowEventTrigger;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::PauseWorkflowProviderEventTriggerRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::pause_event_trigger(&inner, request).await
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
                        let method = PauseEventTriggerSvc(inner);
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
                "/gestalt.provider.v1.WorkflowProvider/ResumeEventTrigger" => {
                    #[allow(non_camel_case_types)]
                    struct ResumeEventTriggerSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<
                            super::ResumeWorkflowProviderEventTriggerRequest,
                        > for ResumeEventTriggerSvc<T>
                    {
                        type Response = super::BoundWorkflowEventTrigger;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::ResumeWorkflowProviderEventTriggerRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::resume_event_trigger(&inner, request).await
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
                        let method = ResumeEventTriggerSvc(inner);
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
                "/gestalt.provider.v1.WorkflowProvider/PutExecutionReference" => {
                    #[allow(non_camel_case_types)]
                    struct PutExecutionReferenceSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<super::PutWorkflowExecutionReferenceRequest>
                        for PutExecutionReferenceSvc<T>
                    {
                        type Response = super::WorkflowExecutionReference;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::PutWorkflowExecutionReferenceRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::put_execution_reference(&inner, request)
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
                        let method = PutExecutionReferenceSvc(inner);
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
                "/gestalt.provider.v1.WorkflowProvider/GetExecutionReference" => {
                    #[allow(non_camel_case_types)]
                    struct GetExecutionReferenceSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<super::GetWorkflowExecutionReferenceRequest>
                        for GetExecutionReferenceSvc<T>
                    {
                        type Response = super::WorkflowExecutionReference;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::GetWorkflowExecutionReferenceRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::get_execution_reference(&inner, request)
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
                        let method = GetExecutionReferenceSvc(inner);
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
                "/gestalt.provider.v1.WorkflowProvider/ListExecutionReferences" => {
                    #[allow(non_camel_case_types)]
                    struct ListExecutionReferencesSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<super::ListWorkflowExecutionReferencesRequest>
                        for ListExecutionReferencesSvc<T>
                    {
                        type Response = super::ListWorkflowExecutionReferencesResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::ListWorkflowExecutionReferencesRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::list_execution_references(&inner, request)
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
                        let method = ListExecutionReferencesSvc(inner);
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
                "/gestalt.provider.v1.WorkflowProvider/PublishEvent" => {
                    #[allow(non_camel_case_types)]
                    struct PublishEventSvc<T: WorkflowProvider>(pub Arc<T>);
                    impl<T: WorkflowProvider>
                        tonic::server::UnaryService<super::PublishWorkflowProviderEventRequest>
                        for PublishEventSvc<T>
                    {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::PublishWorkflowProviderEventRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowProvider>::publish_event(&inner, request).await
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
                        let method = PublishEventSvc(inner);
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
    impl<T> Clone for WorkflowProviderServer<T> {
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
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.WorkflowProvider";
    impl<T> tonic::server::NamedService for WorkflowProviderServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod workflow_host_client {
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
    pub struct WorkflowHostClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl WorkflowHostClient<tonic::transport::Channel> {
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
    impl<T> WorkflowHostClient<T>
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
        ) -> WorkflowHostClient<InterceptedService<T, F>>
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
            WorkflowHostClient::new(InterceptedService::new(inner, interceptor))
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
        pub async fn invoke_operation(
            &mut self,
            request: impl tonic::IntoRequest<super::InvokeWorkflowOperationRequest>,
        ) -> std::result::Result<
            tonic::Response<super::InvokeWorkflowOperationResponse>,
            tonic::Status,
        > {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowHost/InvokeOperation",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowHost",
                "InvokeOperation",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod workflow_host_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with WorkflowHostServer.
    #[async_trait]
    pub trait WorkflowHost: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn invoke_operation(
            &self,
            request: tonic::Request<super::InvokeWorkflowOperationRequest>,
        ) -> std::result::Result<
            tonic::Response<super::InvokeWorkflowOperationResponse>,
            tonic::Status,
        >;
    }
    ///
    #[derive(Debug)]
    pub struct WorkflowHostServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> WorkflowHostServer<T> {
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
    impl<T, B> tonic::codegen::Service<http::Request<B>> for WorkflowHostServer<T>
    where
        T: WorkflowHost,
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
                "/gestalt.provider.v1.WorkflowHost/InvokeOperation" => {
                    #[allow(non_camel_case_types)]
                    struct InvokeOperationSvc<T: WorkflowHost>(pub Arc<T>);
                    impl<T: WorkflowHost>
                        tonic::server::UnaryService<super::InvokeWorkflowOperationRequest>
                        for InvokeOperationSvc<T>
                    {
                        type Response = super::InvokeWorkflowOperationResponse;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::InvokeWorkflowOperationRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowHost>::invoke_operation(&inner, request).await
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
                        let method = InvokeOperationSvc(inner);
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
    impl<T> Clone for WorkflowHostServer<T> {
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
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.WorkflowHost";
    impl<T> tonic::server::NamedService for WorkflowHostServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
/// Generated client implementations.
pub mod workflow_manager_host_client {
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
    pub struct WorkflowManagerHostClient<T> {
        inner: tonic::client::Grpc<T>,
    }
    impl WorkflowManagerHostClient<tonic::transport::Channel> {
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
    impl<T> WorkflowManagerHostClient<T>
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
        ) -> WorkflowManagerHostClient<InterceptedService<T, F>>
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
            WorkflowManagerHostClient::new(InterceptedService::new(inner, interceptor))
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
        pub async fn start_run(
            &mut self,
            request: impl tonic::IntoRequest<super::WorkflowManagerStartRunRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowRun>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowManagerHost/StartRun",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowManagerHost",
                "StartRun",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn signal_run(
            &mut self,
            request: impl tonic::IntoRequest<super::WorkflowManagerSignalRunRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowRunSignal>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowManagerHost/SignalRun",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowManagerHost",
                "SignalRun",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn signal_or_start_run(
            &mut self,
            request: impl tonic::IntoRequest<super::WorkflowManagerSignalOrStartRunRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowRunSignal>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowManagerHost/SignalOrStartRun",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowManagerHost",
                "SignalOrStartRun",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn create_schedule(
            &mut self,
            request: impl tonic::IntoRequest<super::WorkflowManagerCreateScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowSchedule>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowManagerHost/CreateSchedule",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowManagerHost",
                "CreateSchedule",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_schedule(
            &mut self,
            request: impl tonic::IntoRequest<super::WorkflowManagerGetScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowSchedule>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowManagerHost/GetSchedule",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowManagerHost",
                "GetSchedule",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn update_schedule(
            &mut self,
            request: impl tonic::IntoRequest<super::WorkflowManagerUpdateScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowSchedule>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowManagerHost/UpdateSchedule",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowManagerHost",
                "UpdateSchedule",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn delete_schedule(
            &mut self,
            request: impl tonic::IntoRequest<super::WorkflowManagerDeleteScheduleRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowManagerHost/DeleteSchedule",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowManagerHost",
                "DeleteSchedule",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn pause_schedule(
            &mut self,
            request: impl tonic::IntoRequest<super::WorkflowManagerPauseScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowSchedule>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowManagerHost/PauseSchedule",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowManagerHost",
                "PauseSchedule",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn resume_schedule(
            &mut self,
            request: impl tonic::IntoRequest<super::WorkflowManagerResumeScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowSchedule>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowManagerHost/ResumeSchedule",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowManagerHost",
                "ResumeSchedule",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn create_event_trigger(
            &mut self,
            request: impl tonic::IntoRequest<super::WorkflowManagerCreateEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowEventTrigger>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowManagerHost/CreateEventTrigger",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowManagerHost",
                "CreateEventTrigger",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn get_event_trigger(
            &mut self,
            request: impl tonic::IntoRequest<super::WorkflowManagerGetEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowEventTrigger>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowManagerHost/GetEventTrigger",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowManagerHost",
                "GetEventTrigger",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn update_event_trigger(
            &mut self,
            request: impl tonic::IntoRequest<super::WorkflowManagerUpdateEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowEventTrigger>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowManagerHost/UpdateEventTrigger",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowManagerHost",
                "UpdateEventTrigger",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn delete_event_trigger(
            &mut self,
            request: impl tonic::IntoRequest<super::WorkflowManagerDeleteEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowManagerHost/DeleteEventTrigger",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowManagerHost",
                "DeleteEventTrigger",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn pause_event_trigger(
            &mut self,
            request: impl tonic::IntoRequest<super::WorkflowManagerPauseEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowEventTrigger>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowManagerHost/PauseEventTrigger",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowManagerHost",
                "PauseEventTrigger",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn resume_event_trigger(
            &mut self,
            request: impl tonic::IntoRequest<super::WorkflowManagerResumeEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowEventTrigger>, tonic::Status>
        {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowManagerHost/ResumeEventTrigger",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowManagerHost",
                "ResumeEventTrigger",
            ));
            self.inner.unary(req, path, codec).await
        }
        ///
        pub async fn publish_event(
            &mut self,
            request: impl tonic::IntoRequest<super::WorkflowManagerPublishEventRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowEvent>, tonic::Status> {
            self.inner.ready().await.map_err(|e| {
                tonic::Status::unknown(format!("Service was not ready: {}", e.into()))
            })?;
            let codec = tonic_prost::ProstCodec::default();
            let path = http::uri::PathAndQuery::from_static(
                "/gestalt.provider.v1.WorkflowManagerHost/PublishEvent",
            );
            let mut req = request.into_request();
            req.extensions_mut().insert(GrpcMethod::new(
                "gestalt.provider.v1.WorkflowManagerHost",
                "PublishEvent",
            ));
            self.inner.unary(req, path, codec).await
        }
    }
}
/// Generated server implementations.
pub mod workflow_manager_host_server {
    #![allow(
        unused_variables,
        dead_code,
        missing_docs,
        clippy::wildcard_imports,
        clippy::let_unit_value
    )]
    use tonic::codegen::*;
    /// Generated trait containing gRPC methods that should be implemented for use with WorkflowManagerHostServer.
    #[async_trait]
    pub trait WorkflowManagerHost: std::marker::Send + std::marker::Sync + 'static {
        ///
        async fn start_run(
            &self,
            request: tonic::Request<super::WorkflowManagerStartRunRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowRun>, tonic::Status>;
        ///
        async fn signal_run(
            &self,
            request: tonic::Request<super::WorkflowManagerSignalRunRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowRunSignal>, tonic::Status>;
        ///
        async fn signal_or_start_run(
            &self,
            request: tonic::Request<super::WorkflowManagerSignalOrStartRunRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowRunSignal>, tonic::Status>;
        ///
        async fn create_schedule(
            &self,
            request: tonic::Request<super::WorkflowManagerCreateScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowSchedule>, tonic::Status>;
        ///
        async fn get_schedule(
            &self,
            request: tonic::Request<super::WorkflowManagerGetScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowSchedule>, tonic::Status>;
        ///
        async fn update_schedule(
            &self,
            request: tonic::Request<super::WorkflowManagerUpdateScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowSchedule>, tonic::Status>;
        ///
        async fn delete_schedule(
            &self,
            request: tonic::Request<super::WorkflowManagerDeleteScheduleRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn pause_schedule(
            &self,
            request: tonic::Request<super::WorkflowManagerPauseScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowSchedule>, tonic::Status>;
        ///
        async fn resume_schedule(
            &self,
            request: tonic::Request<super::WorkflowManagerResumeScheduleRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowSchedule>, tonic::Status>;
        ///
        async fn create_event_trigger(
            &self,
            request: tonic::Request<super::WorkflowManagerCreateEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowEventTrigger>, tonic::Status>;
        ///
        async fn get_event_trigger(
            &self,
            request: tonic::Request<super::WorkflowManagerGetEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowEventTrigger>, tonic::Status>;
        ///
        async fn update_event_trigger(
            &self,
            request: tonic::Request<super::WorkflowManagerUpdateEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowEventTrigger>, tonic::Status>;
        ///
        async fn delete_event_trigger(
            &self,
            request: tonic::Request<super::WorkflowManagerDeleteEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<()>, tonic::Status>;
        ///
        async fn pause_event_trigger(
            &self,
            request: tonic::Request<super::WorkflowManagerPauseEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowEventTrigger>, tonic::Status>;
        ///
        async fn resume_event_trigger(
            &self,
            request: tonic::Request<super::WorkflowManagerResumeEventTriggerRequest>,
        ) -> std::result::Result<tonic::Response<super::ManagedWorkflowEventTrigger>, tonic::Status>;
        ///
        async fn publish_event(
            &self,
            request: tonic::Request<super::WorkflowManagerPublishEventRequest>,
        ) -> std::result::Result<tonic::Response<super::WorkflowEvent>, tonic::Status>;
    }
    ///
    #[derive(Debug)]
    pub struct WorkflowManagerHostServer<T> {
        inner: Arc<T>,
        accept_compression_encodings: EnabledCompressionEncodings,
        send_compression_encodings: EnabledCompressionEncodings,
        max_decoding_message_size: Option<usize>,
        max_encoding_message_size: Option<usize>,
    }
    impl<T> WorkflowManagerHostServer<T> {
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
    impl<T, B> tonic::codegen::Service<http::Request<B>> for WorkflowManagerHostServer<T>
    where
        T: WorkflowManagerHost,
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
                "/gestalt.provider.v1.WorkflowManagerHost/StartRun" => {
                    #[allow(non_camel_case_types)]
                    struct StartRunSvc<T: WorkflowManagerHost>(pub Arc<T>);
                    impl<T: WorkflowManagerHost>
                        tonic::server::UnaryService<super::WorkflowManagerStartRunRequest>
                        for StartRunSvc<T>
                    {
                        type Response = super::ManagedWorkflowRun;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::WorkflowManagerStartRunRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowManagerHost>::start_run(&inner, request).await
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
                "/gestalt.provider.v1.WorkflowManagerHost/SignalRun" => {
                    #[allow(non_camel_case_types)]
                    struct SignalRunSvc<T: WorkflowManagerHost>(pub Arc<T>);
                    impl<T: WorkflowManagerHost>
                        tonic::server::UnaryService<super::WorkflowManagerSignalRunRequest>
                        for SignalRunSvc<T>
                    {
                        type Response = super::ManagedWorkflowRunSignal;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::WorkflowManagerSignalRunRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowManagerHost>::signal_run(&inner, request).await
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
                "/gestalt.provider.v1.WorkflowManagerHost/SignalOrStartRun" => {
                    #[allow(non_camel_case_types)]
                    struct SignalOrStartRunSvc<T: WorkflowManagerHost>(pub Arc<T>);
                    impl<T: WorkflowManagerHost>
                        tonic::server::UnaryService<super::WorkflowManagerSignalOrStartRunRequest>
                        for SignalOrStartRunSvc<T>
                    {
                        type Response = super::ManagedWorkflowRunSignal;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::WorkflowManagerSignalOrStartRunRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowManagerHost>::signal_or_start_run(&inner, request)
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
                "/gestalt.provider.v1.WorkflowManagerHost/CreateSchedule" => {
                    #[allow(non_camel_case_types)]
                    struct CreateScheduleSvc<T: WorkflowManagerHost>(pub Arc<T>);
                    impl<T: WorkflowManagerHost>
                        tonic::server::UnaryService<super::WorkflowManagerCreateScheduleRequest>
                        for CreateScheduleSvc<T>
                    {
                        type Response = super::ManagedWorkflowSchedule;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::WorkflowManagerCreateScheduleRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowManagerHost>::create_schedule(&inner, request).await
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
                        let method = CreateScheduleSvc(inner);
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
                "/gestalt.provider.v1.WorkflowManagerHost/GetSchedule" => {
                    #[allow(non_camel_case_types)]
                    struct GetScheduleSvc<T: WorkflowManagerHost>(pub Arc<T>);
                    impl<T: WorkflowManagerHost>
                        tonic::server::UnaryService<super::WorkflowManagerGetScheduleRequest>
                        for GetScheduleSvc<T>
                    {
                        type Response = super::ManagedWorkflowSchedule;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::WorkflowManagerGetScheduleRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowManagerHost>::get_schedule(&inner, request).await
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
                        let method = GetScheduleSvc(inner);
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
                "/gestalt.provider.v1.WorkflowManagerHost/UpdateSchedule" => {
                    #[allow(non_camel_case_types)]
                    struct UpdateScheduleSvc<T: WorkflowManagerHost>(pub Arc<T>);
                    impl<T: WorkflowManagerHost>
                        tonic::server::UnaryService<super::WorkflowManagerUpdateScheduleRequest>
                        for UpdateScheduleSvc<T>
                    {
                        type Response = super::ManagedWorkflowSchedule;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::WorkflowManagerUpdateScheduleRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowManagerHost>::update_schedule(&inner, request).await
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
                        let method = UpdateScheduleSvc(inner);
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
                "/gestalt.provider.v1.WorkflowManagerHost/DeleteSchedule" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteScheduleSvc<T: WorkflowManagerHost>(pub Arc<T>);
                    impl<T: WorkflowManagerHost>
                        tonic::server::UnaryService<super::WorkflowManagerDeleteScheduleRequest>
                        for DeleteScheduleSvc<T>
                    {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::WorkflowManagerDeleteScheduleRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowManagerHost>::delete_schedule(&inner, request).await
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
                        let method = DeleteScheduleSvc(inner);
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
                "/gestalt.provider.v1.WorkflowManagerHost/PauseSchedule" => {
                    #[allow(non_camel_case_types)]
                    struct PauseScheduleSvc<T: WorkflowManagerHost>(pub Arc<T>);
                    impl<T: WorkflowManagerHost>
                        tonic::server::UnaryService<super::WorkflowManagerPauseScheduleRequest>
                        for PauseScheduleSvc<T>
                    {
                        type Response = super::ManagedWorkflowSchedule;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::WorkflowManagerPauseScheduleRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowManagerHost>::pause_schedule(&inner, request).await
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
                        let method = PauseScheduleSvc(inner);
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
                "/gestalt.provider.v1.WorkflowManagerHost/ResumeSchedule" => {
                    #[allow(non_camel_case_types)]
                    struct ResumeScheduleSvc<T: WorkflowManagerHost>(pub Arc<T>);
                    impl<T: WorkflowManagerHost>
                        tonic::server::UnaryService<super::WorkflowManagerResumeScheduleRequest>
                        for ResumeScheduleSvc<T>
                    {
                        type Response = super::ManagedWorkflowSchedule;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::WorkflowManagerResumeScheduleRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowManagerHost>::resume_schedule(&inner, request).await
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
                        let method = ResumeScheduleSvc(inner);
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
                "/gestalt.provider.v1.WorkflowManagerHost/CreateEventTrigger" => {
                    #[allow(non_camel_case_types)]
                    struct CreateEventTriggerSvc<T: WorkflowManagerHost>(pub Arc<T>);
                    impl<T: WorkflowManagerHost>
                        tonic::server::UnaryService<super::WorkflowManagerCreateEventTriggerRequest>
                        for CreateEventTriggerSvc<T>
                    {
                        type Response = super::ManagedWorkflowEventTrigger;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::WorkflowManagerCreateEventTriggerRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowManagerHost>::create_event_trigger(&inner, request)
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
                        let method = CreateEventTriggerSvc(inner);
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
                "/gestalt.provider.v1.WorkflowManagerHost/GetEventTrigger" => {
                    #[allow(non_camel_case_types)]
                    struct GetEventTriggerSvc<T: WorkflowManagerHost>(pub Arc<T>);
                    impl<T: WorkflowManagerHost>
                        tonic::server::UnaryService<super::WorkflowManagerGetEventTriggerRequest>
                        for GetEventTriggerSvc<T>
                    {
                        type Response = super::ManagedWorkflowEventTrigger;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::WorkflowManagerGetEventTriggerRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowManagerHost>::get_event_trigger(&inner, request).await
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
                        let method = GetEventTriggerSvc(inner);
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
                "/gestalt.provider.v1.WorkflowManagerHost/UpdateEventTrigger" => {
                    #[allow(non_camel_case_types)]
                    struct UpdateEventTriggerSvc<T: WorkflowManagerHost>(pub Arc<T>);
                    impl<T: WorkflowManagerHost>
                        tonic::server::UnaryService<super::WorkflowManagerUpdateEventTriggerRequest>
                        for UpdateEventTriggerSvc<T>
                    {
                        type Response = super::ManagedWorkflowEventTrigger;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::WorkflowManagerUpdateEventTriggerRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowManagerHost>::update_event_trigger(&inner, request)
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
                        let method = UpdateEventTriggerSvc(inner);
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
                "/gestalt.provider.v1.WorkflowManagerHost/DeleteEventTrigger" => {
                    #[allow(non_camel_case_types)]
                    struct DeleteEventTriggerSvc<T: WorkflowManagerHost>(pub Arc<T>);
                    impl<T: WorkflowManagerHost>
                        tonic::server::UnaryService<super::WorkflowManagerDeleteEventTriggerRequest>
                        for DeleteEventTriggerSvc<T>
                    {
                        type Response = ();
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::WorkflowManagerDeleteEventTriggerRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowManagerHost>::delete_event_trigger(&inner, request)
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
                        let method = DeleteEventTriggerSvc(inner);
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
                "/gestalt.provider.v1.WorkflowManagerHost/PauseEventTrigger" => {
                    #[allow(non_camel_case_types)]
                    struct PauseEventTriggerSvc<T: WorkflowManagerHost>(pub Arc<T>);
                    impl<T: WorkflowManagerHost>
                        tonic::server::UnaryService<super::WorkflowManagerPauseEventTriggerRequest>
                        for PauseEventTriggerSvc<T>
                    {
                        type Response = super::ManagedWorkflowEventTrigger;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::WorkflowManagerPauseEventTriggerRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowManagerHost>::pause_event_trigger(&inner, request)
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
                        let method = PauseEventTriggerSvc(inner);
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
                "/gestalt.provider.v1.WorkflowManagerHost/ResumeEventTrigger" => {
                    #[allow(non_camel_case_types)]
                    struct ResumeEventTriggerSvc<T: WorkflowManagerHost>(pub Arc<T>);
                    impl<T: WorkflowManagerHost>
                        tonic::server::UnaryService<super::WorkflowManagerResumeEventTriggerRequest>
                        for ResumeEventTriggerSvc<T>
                    {
                        type Response = super::ManagedWorkflowEventTrigger;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<
                                super::WorkflowManagerResumeEventTriggerRequest,
                            >,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowManagerHost>::resume_event_trigger(&inner, request)
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
                        let method = ResumeEventTriggerSvc(inner);
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
                "/gestalt.provider.v1.WorkflowManagerHost/PublishEvent" => {
                    #[allow(non_camel_case_types)]
                    struct PublishEventSvc<T: WorkflowManagerHost>(pub Arc<T>);
                    impl<T: WorkflowManagerHost>
                        tonic::server::UnaryService<super::WorkflowManagerPublishEventRequest>
                        for PublishEventSvc<T>
                    {
                        type Response = super::WorkflowEvent;
                        type Future = BoxFuture<tonic::Response<Self::Response>, tonic::Status>;
                        fn call(
                            &mut self,
                            request: tonic::Request<super::WorkflowManagerPublishEventRequest>,
                        ) -> Self::Future {
                            let inner = Arc::clone(&self.0);
                            let fut = async move {
                                <T as WorkflowManagerHost>::publish_event(&inner, request).await
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
                        let method = PublishEventSvc(inner);
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
    impl<T> Clone for WorkflowManagerHostServer<T> {
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
    pub const SERVICE_NAME: &str = "gestalt.provider.v1.WorkflowManagerHost";
    impl<T> tonic::server::NamedService for WorkflowManagerHostServer<T> {
        const NAME: &'static str = SERVICE_NAME;
    }
}
