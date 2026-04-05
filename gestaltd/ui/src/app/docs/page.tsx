import type { Metadata } from "next";
import DocsClient from "./DocsClient";

export const metadata: Metadata = {
  title: "Docs",
  description:
    "User documentation for the Gestalt CLI, API tokens, and MCP endpoint.",
};

export default function DocsPage() {
  return <DocsClient />;
}
