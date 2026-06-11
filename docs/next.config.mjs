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
      // Same comment-color lift as the registry renderer: vitesse-light's
      // #a0ada0 comments are 2.3:1 on our cream pre background.
      colorReplacements: {
        "vitesse-light": { "#a0ada0": "#76705f" },
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
