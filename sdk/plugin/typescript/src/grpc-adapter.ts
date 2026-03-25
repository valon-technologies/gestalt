import * as grpc from "@grpc/grpc-js";
import type { IMessageType } from "@protobuf-ts/runtime";
import { ProviderPlugin, RuntimePlugin, RuntimeHost } from "../gen/v1/plugin";
import type { ServiceType } from "@protobuf-ts/runtime-rpc";

function makeSerializer<T extends object>(type: IMessageType<T>): grpc.serialize<T> {
  return (value: T): Buffer => {
    return Buffer.from(type.toBinary(value));
  };
}

function makeDeserializer<T extends object>(type: IMessageType<T>): grpc.deserialize<T> {
  return (bytes: Buffer): T => {
    return type.fromBinary(bytes);
  };
}

interface MethodDef {
  path: string;
  requestStream: false;
  responseStream: false;
  requestSerialize: grpc.serialize<any>;
  requestDeserialize: grpc.deserialize<any>;
  responseSerialize: grpc.serialize<any>;
  responseDeserialize: grpc.deserialize<any>;
}

function buildServiceDefinition(
  serviceType: ServiceType,
): { [key: string]: MethodDef } {
  const def: { [key: string]: MethodDef } = {};
  for (const method of serviceType.methods) {
    const path = `/${serviceType.typeName}/${method.name}`;
    def[method.name] = {
      path,
      requestStream: false as const,
      responseStream: false as const,
      requestSerialize: makeSerializer(method.I as IMessageType<any>),
      requestDeserialize: makeDeserializer(method.I as IMessageType<any>),
      responseSerialize: makeSerializer(method.O as IMessageType<any>),
      responseDeserialize: makeDeserializer(method.O as IMessageType<any>),
    };
  }
  return def;
}

export const providerPluginDefinition = buildServiceDefinition(ProviderPlugin);
export const runtimePluginDefinition = buildServiceDefinition(RuntimePlugin);
export const runtimeHostDefinition = buildServiceDefinition(RuntimeHost);
