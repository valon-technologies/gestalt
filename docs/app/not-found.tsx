export default function NotFound() {
  return (
    <main className="shell-not-found">
      <h1>404</h1>
      <p>This page could not be found.</p>
      <p>
        <a className="shell-link" href="/">
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
