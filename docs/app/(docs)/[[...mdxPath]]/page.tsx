import { notFound } from "next/navigation";
import { generateStaticParamsFor, importPage } from "nextra/pages";
import { useMDXComponents as getMDXComponents } from "../../../mdx-components";

type PageParams = {
  mdxPath?: string[];
};

export const generateStaticParams = generateStaticParamsFor("mdxPath");

// Serve only the routes that prerender into the static export, mirroring
// nginx in production. Anything else (e.g. /versions.json, which exists only
// in composed production output) must 404 before reaching importPage, which
// logs a module-resolution error for unknown routes.
async function loadPage(mdxPath: string[] = []) {
  let requested: string;
  try {
    requested = decodeURIComponent(mdxPath.join("/"));
  } catch {
    // Malformed percent-escapes cannot name a known route.
    notFound();
  }
  const params = await generateStaticParams();
  const known = params.some(
    (param) => [param.mdxPath ?? []].flat().join("/") === requested,
  );
  if (!known) {
    notFound();
  }
  return importPage(mdxPath);
}

export async function generateMetadata(props: {
  params: Promise<PageParams>;
}) {
  const params = await props.params;
  const { metadata } = await loadPage(params.mdxPath);
  return metadata;
}

const Wrapper = getMDXComponents().wrapper;

export default async function Page(props: {
  params: Promise<PageParams>;
}) {
  const params = await props.params;
  const { default: MDXContent, metadata, sourceCode, toc } = await loadPage(
    params.mdxPath,
  );

  return (
    <Wrapper toc={toc} metadata={metadata} sourceCode={sourceCode}>
      <MDXContent {...props} params={params} />
    </Wrapper>
  );
}
