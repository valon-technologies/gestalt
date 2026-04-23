import { writeFileSync } from "node:fs";

import YAML from "yaml";

export type HTTPSecuritySchemeType =
  | "hmac"
  | "apiKey"
  | "http"
  | "none";

export type HTTPIn = "header" | "query";

export type HTTPAuthScheme = "basic" | "bearer";

export interface HTTPSecretRef {
  env?: string;
  secret?: string;
}

export interface HTTPSecurityScheme {
  type?: HTTPSecuritySchemeType;
  description?: string;
  signatureHeader?: string;
  signaturePrefix?: string;
  payloadTemplate?: string;
  timestampHeader?: string;
  maxAgeSeconds?: number;
  name?: string;
  in?: HTTPIn;
  scheme?: HTTPAuthScheme;
  secret?: HTTPSecretRef;
}

export interface HTTPMediaType {}

export interface HTTPRequestBody {
  required?: boolean;
  content?: Record<string, HTTPMediaType>;
}

export interface HTTPAck {
  status?: number;
  headers?: Record<string, string>;
  body?: any;
}

export interface HTTPBinding {
  path: string;
  method: string;
  credentialMode?: "none" | "user";
  requestBody?: HTTPRequestBody;
  security: string;
  target: string;
  ack?: HTTPAck;
}

export interface PluginManifestMetadata {
  securitySchemes?: Record<string, HTTPSecurityScheme>;
  http?: Record<string, HTTPBinding>;
}

export function hasPluginManifestMetadata(
  metadata: PluginManifestMetadata | null | undefined,
): boolean {
  return !!(
    metadata &&
    ((metadata.securitySchemes &&
      Object.keys(metadata.securitySchemes).length > 0) ||
      (metadata.http && Object.keys(metadata.http).length > 0))
  );
}

export function manifestMetadataToYaml(
  metadata: PluginManifestMetadata | Record<string, unknown>,
): string {
  return YAML.stringify(toManifestMetadataJsonObject(metadata));
}

export function writeManifestMetadataYaml(
  path: string,
  metadata: PluginManifestMetadata | Record<string, unknown>,
): void {
  writeFileSync(path, manifestMetadataToYaml(metadata), "utf8");
}

function toManifestMetadataJsonObject(
  metadata: PluginManifestMetadata | Record<string, unknown>,
): Record<string, unknown> {
  if (!("securitySchemes" in metadata) && !("http" in metadata)) {
    return {
      ...metadata,
    };
  }

  const typedMetadata = metadata as PluginManifestMetadata;
  const output: Record<string, unknown> = {};
  if (
    typedMetadata.securitySchemes &&
    Object.keys(typedMetadata.securitySchemes).length > 0
  ) {
    output.securitySchemes = typedMetadata.securitySchemes;
  }
  if (typedMetadata.http && Object.keys(typedMetadata.http).length > 0) {
    output.http = typedMetadata.http;
  }
  return output;
}
