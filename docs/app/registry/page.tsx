import type { Metadata } from "next";
import RegistryApp from "./registry-app";

const defaultCatalogUrl =
  "https://raw.githubusercontent.com/valon-technologies/gestalt-providers/main/registry/catalog.json";

export const metadata: Metadata = {
  title: "Provider Registry",
  description: "Browse installable Gestalt provider packages.",
};

export default function RegistryPage() {
  return (
    <RegistryApp
      catalogUrl={
        process.env.NEXT_PUBLIC_PROVIDER_REGISTRY_CATALOG_URL ??
        defaultCatalogUrl
      }
    />
  );
}
