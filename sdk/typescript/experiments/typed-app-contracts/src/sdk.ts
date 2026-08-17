export type ToolHandler<Input, Output> = (input: Input) => Output | Promise<Output>;

export interface Tool<Input, Output> {
  readonly description: string;
  readonly handler: ToolHandler<Input, Output>;
}

export interface App<Tools extends Record<string, Tool<unknown, unknown>>> {
  readonly tools: Tools;
}

export function tool<Input, Output>(definition: {
  description?: string;
  handler: ToolHandler<Input, Output>;
}): Tool<Input, Output> {
  return {
    description: definition.description ?? "",
    handler: definition.handler,
  };
}

export function app<Tools extends Record<string, Tool<any, any>>>(definition: {
  tools: Tools;
}): App<Tools> {
  return definition;
}
