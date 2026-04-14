use std::sync::Arc;
use std::time::Duration;

use tonic::{Request as GrpcRequest, Response as GrpcResponse, Status};

use crate::cache::{CacheEntry, CacheProvider, CacheSetOptions};
use crate::generated::v1::cache_server::Cache as CacheGrpc;
use crate::generated::v1::{
    CacheDeleteManyRequest, CacheDeleteManyResponse, CacheDeleteRequest, CacheDeleteResponse,
    CacheGetManyRequest, CacheGetManyResponse, CacheGetRequest, CacheGetResponse, CacheResult,
    CacheSetManyRequest, CacheSetRequest, CacheTouchRequest, CacheTouchResponse,
};
use crate::rpc_status::rpc_status;

#[derive(Clone)]
pub struct CacheRpcServer<P> {
    cache: Arc<P>,
}

impl<P> CacheRpcServer<P> {
    pub fn new(cache: Arc<P>) -> Self {
        Self { cache }
    }
}

#[tonic::async_trait]
impl<P> CacheGrpc for CacheRpcServer<P>
where
    P: CacheProvider,
{
    async fn get(
        &self,
        request: GrpcRequest<CacheGetRequest>,
    ) -> std::result::Result<GrpcResponse<CacheGetResponse>, Status> {
        let request = request.into_inner();
        let value = self
            .cache
            .get(&request.key)
            .await
            .map_err(|error| rpc_status("get cache entry", error))?;
        match value {
            Some(value) => Ok(GrpcResponse::new(CacheGetResponse { found: true, value })),
            None => Ok(GrpcResponse::new(CacheGetResponse {
                found: false,
                value: Vec::new(),
            })),
        }
    }

    async fn get_many(
        &self,
        request: GrpcRequest<CacheGetManyRequest>,
    ) -> std::result::Result<GrpcResponse<CacheGetManyResponse>, Status> {
        let request = request.into_inner();
        let values = self
            .cache
            .get_many(&request.keys)
            .await
            .map_err(|error| rpc_status("get many cache entries", error))?;
        let entries = request
            .keys
            .into_iter()
            .map(|key| match values.get(&key) {
                Some(value) => CacheResult {
                    key,
                    found: true,
                    value: value.clone(),
                },
                None => CacheResult {
                    key,
                    found: false,
                    value: Vec::new(),
                },
            })
            .collect();
        Ok(GrpcResponse::new(CacheGetManyResponse { entries }))
    }

    async fn set(
        &self,
        request: GrpcRequest<CacheSetRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        let request = request.into_inner();
        self.cache
            .set(
                &request.key,
                &request.value,
                CacheSetOptions {
                    ttl: duration_from_proto(request.ttl)?,
                },
            )
            .await
            .map_err(|error| rpc_status("set cache entry", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn set_many(
        &self,
        request: GrpcRequest<CacheSetManyRequest>,
    ) -> std::result::Result<GrpcResponse<()>, Status> {
        let request = request.into_inner();
        let entries: Vec<CacheEntry> = request
            .entries
            .into_iter()
            .map(|entry| CacheEntry {
                key: entry.key,
                value: entry.value,
            })
            .collect();
        self.cache
            .set_many(
                &entries,
                CacheSetOptions {
                    ttl: duration_from_proto(request.ttl)?,
                },
            )
            .await
            .map_err(|error| rpc_status("set many cache entries", error))?;
        Ok(GrpcResponse::new(()))
    }

    async fn delete(
        &self,
        request: GrpcRequest<CacheDeleteRequest>,
    ) -> std::result::Result<GrpcResponse<CacheDeleteResponse>, Status> {
        let request = request.into_inner();
        let deleted = self
            .cache
            .delete(&request.key)
            .await
            .map_err(|error| rpc_status("delete cache entry", error))?;
        Ok(GrpcResponse::new(CacheDeleteResponse { deleted }))
    }

    async fn delete_many(
        &self,
        request: GrpcRequest<CacheDeleteManyRequest>,
    ) -> std::result::Result<GrpcResponse<CacheDeleteManyResponse>, Status> {
        let deleted = self
            .cache
            .delete_many(&request.into_inner().keys)
            .await
            .map_err(|error| rpc_status("delete many cache entries", error))?;
        Ok(GrpcResponse::new(CacheDeleteManyResponse { deleted }))
    }

    async fn touch(
        &self,
        request: GrpcRequest<CacheTouchRequest>,
    ) -> std::result::Result<GrpcResponse<CacheTouchResponse>, Status> {
        let request = request.into_inner();
        let ttl = duration_from_proto(request.ttl)?.unwrap_or_default();
        let touched = self
            .cache
            .touch(&request.key, ttl)
            .await
            .map_err(|error| rpc_status("touch cache entry", error))?;
        Ok(GrpcResponse::new(CacheTouchResponse { touched }))
    }
}

fn duration_from_proto(
    ttl: Option<prost_types::Duration>,
) -> std::result::Result<Option<Duration>, Status> {
    let Some(ttl) = ttl else {
        return Ok(None);
    };
    if ttl.seconds < 0 {
        return Err(Status::invalid_argument("ttl must be non-negative"));
    }
    if ttl.nanos < 0 || ttl.nanos >= 1_000_000_000 {
        return Err(Status::invalid_argument("ttl nanos must be in [0, 1e9)"));
    }
    let seconds =
        u64::try_from(ttl.seconds).map_err(|_| Status::invalid_argument("ttl is too large"))?;
    Ok(Some(Duration::new(seconds, ttl.nanos as u32)))
}
