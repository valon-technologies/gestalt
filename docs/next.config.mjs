import nextra from "nextra";

const withNextra = nextra({});
const basePath = process.env.GESTALT_DOCS_BASE_PATH || "";

export default withNextra({
  ...(process.env.NODE_ENV === "production" ? { output: "export" } : {}),
  ...(basePath ? { basePath, assetPrefix: basePath } : {}),
  images: { unoptimized: true },
});
