import nextra from "nextra";

const withNextra = nextra({
  mdxOptions: {
    rehypePrettyCodeOptions: {
      // Vitesse pair chosen in RES-20260610-007; the registry's client
      // renderer uses the same themes via shiki-code.tsx.
      theme: {
        light: "vitesse-light",
        dark: "vitesse-dark",
      },
    },
  },
});
const basePath = process.env.GESTALT_DOCS_BASE_PATH || "";

export default withNextra({
  ...(process.env.NODE_ENV === "production" ? { output: "export" } : {}),
  ...(basePath ? { basePath, assetPrefix: basePath } : {}),
  images: { unoptimized: true },
});
