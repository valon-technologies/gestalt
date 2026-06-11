// Versioned builds serve this page under /dev, /latest, or /versions/*;
// recovery links must stay inside that tree (plain anchors skip Next's
// basePath prefixing). The registry is unversioned at the root.
const docsHref = `${process.env.GESTALT_DOCS_BASE_PATH || ""}/`;

export default function NotFound() {
  return (
    <main className="shell-not-found">
      <h1>404</h1>
      <p>This page could not be found.</p>
      <p>
        <a className="shell-link" href={docsHref}>
          Documentation
        </a>{" "}
        ·{" "}
        <a className="shell-link" href="/registry">
          Provider registry
        </a>
      </p>
    </main>
  );
}
