/**
 * Public Gestalt transport interface shared by REST and gRPC clients.
 */

import type { DescMessage, Message } from "@bufbuild/protobuf";

import type { PublicMethod } from "./generated/methods.ts";

export interface PublicTransport {
  unary<Res extends Message>(
    method: PublicMethod,
    request: Message,
    requestSchema: DescMessage,
    responseSchema: DescMessage,
  ): Promise<Res>;
}
