import nextra from "nextra";

const withNextra = nextra({});

export default withNextra({
  ...(process.env.NODE_ENV === "production" ? { output: "export" } : {}),
  images: { unoptimized: true },
});
