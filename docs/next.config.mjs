import nextra from "nextra";

const withNextra = nextra({
  mdxOptions: {
    rehypePrettyCodeOptions: {
      // Vitesse pair for docs MDX code blocks; the registry's client
      // renderer (app/registry/shiki-code.tsx) uses the same themes so
      // code looks identical on both sides of the site.
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
