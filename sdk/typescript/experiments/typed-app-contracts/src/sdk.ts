import type { z } from "zod";

export type ToolHandler<Input extends z.ZodType, Output extends z.ZodType> = (
  input: z.output<Input>,
) => z.output<Output> | Promise<z.output<Output>>;

export interface Tool<Input extends z.ZodType, Output extends z.ZodType> {
  readonly description: string;
  readonly input: Input;
  readonly output: Output;
  readonly handler: ToolHandler<Input, Output>;
}

export interface App<Tools extends Record<string, Tool<any, any>>> {
  readonly tools: Tools;
}

export function tool<const Input extends z.ZodType, const Output extends z.ZodType>(definition: {
  description?: string;
  input: Input;
  output: Output;
  handler: ToolHandler<Input, Output>;
}): Tool<Input, Output> {
  return { ...definition, description: definition.description ?? "" };
}

export function app<Tools extends Record<string, Tool<any, any>>>(definition: {
  tools: Tools;
}): App<Tools> {
  return definition;
}
