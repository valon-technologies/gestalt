// Identity is the canonical alias client for the Authentication wire service.
// It is a handwritten alias over the generated Authentication client so the
// proto service, host binding, and wire protocol remain "authentication" for
// compatibility, while the public SDK surface exposes the canonical
// "identity" naming.

/**
 * Generated native types and client alias for the identity provider.
 *
 * @module services/identity
 */

export {
  Authentication as Identity,
  type AuthorizeRequest,
  type AuthorizeResponse,
  type GetGrantRequest,
  type GetGrantResponse,
  type GrantScope,
  type IntrospectRequest,
  type IntrospectResponse,
  type ListGrantsRequest,
  type ListGrantsResponse,
  type RevokeGrantRequest,
  type RevokeGrantResponse,
  type TokenRequest,
  type TokenResponse,
  type UserInfoRequest,
  type UserInfoResponse,
} from "./authentication.ts";
