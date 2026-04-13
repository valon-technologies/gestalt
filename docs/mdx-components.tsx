import type { MDXComponents } from "mdx/types";
import type { ComponentProps } from "react";
import { Pre } from "nextra/components";
import { useMDXComponents as getThemeComponents } from "nextra-theme-docs";

function CodeBlock(props: ComponentProps<typeof Pre>) {
  return <Pre {...props} data-copy={props["data-copy"] ?? ""} />;
}

export function useMDXComponents(components: MDXComponents = {}) {
  return {
    ...getThemeComponents(),
    pre: CodeBlock,
    ...components,
  };
}
