import Link from "next/link";

// Versioned builds serve this page under /dev, /latest, or /versions/*.
// The docs link is a next/link so basePath keeps recovery inside the
// current tree while the payload stays unprefixed (the versioned-build
// audit forbids base-prefixed internal links). The registry is
// unversioned at the root, so it stays a plain anchor.
export default function NotFound() {
  return (
    <main className="shell-not-found">
      <h1>404</h1>
      <p>This page could not be found.</p>
      <p>
        <Link className="shell-link" href="/">
          Documentation
        </Link>{" "}
        ·{" "}
        <a className="shell-link" href="/registry">
          Provider registry
        </a>
      </p>
    </main>
  );
}
