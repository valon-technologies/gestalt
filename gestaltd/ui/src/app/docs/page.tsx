import Link from "next/link";
import type { Metadata } from "next";
import Nav from "@/components/Nav";

export const metadata: Metadata = {
  title: "Docs",
  description: "User documentation for the Gestalt CLI, API tokens, and MCP endpoint.",
};

const quickLinks = [
  { href: "#install", label: "Install the CLI" },
  { href: "#connect", label: "Connect integrations" },
  { href: "#invoke", label: "Invoke operations" },
  { href: "#mcp", label: "Connect MCP" },
];

const setupWays = [
  {
    title: "Use the setup wizard",
    copy: "Best for a first login. It stores the server URL and can launch the browser auth flow.",
    code: "gestalt init",
  },
  {
    title: "Set the server URL directly",
    copy: "Useful when you already know the endpoint and want a non-interactive setup.",
    code: "gestalt config set url https://gestalt.example.com",
  },
  {
    title: "Override per shell or project",
    copy: "Use an environment variable for one shell, or create `.gestalt.json` for a repo-local default.",
    code: "export GESTALT_URL=https://gestalt.example.com",
  },
];

const tokenCommands = [
  "gestalt tokens create --name automation",
  "gestalt tokens list",
  "gestalt tokens revoke <token-id>",
];

const issues = [
  {
    title: "The CLI says you are not authenticated",
    copy: "Run `gestalt auth login`, or set `GESTALT_API_KEY` if your team gave you a token directly.",
  },
  {
    title: "An integration has more than one connection",
    copy: "Pass `--connection <name>` or `--instance <name>` so Gestalt can resolve the right credentials.",
  },
  {
    title: "`/mcp` is missing or has no tools",
    copy: "At least one integration must have MCP enabled, and that integration usually needs to be connected before tools appear.",
  },
];

export default function DocsPage() {
  return (
    <div className="min-h-screen">
      <Nav />
      <main className="mx-auto max-w-5xl px-6 py-12">
        <section className="animate-fade-in-up">
          <span className="label-text">User Docs</span>
          <h1 className="mt-3 max-w-3xl text-4xl font-heading font-bold text-primary sm:text-5xl">
            Use Gestalt from the terminal, browser, or any MCP-aware client.
          </h1>
          <p className="mt-5 max-w-3xl text-base text-muted sm:text-lg">
            This page covers the end-user workflows for Gestalt: installing the{" "}
            <code className="font-mono text-sm text-primary">gestalt</code> CLI,
            signing in, connecting integrations, invoking operations, minting
            API tokens, and pointing an MCP client at the{" "}
            <code className="font-mono text-sm text-primary">/mcp</code>{" "}
            endpoint exposed by <code className="font-mono text-sm text-primary">gestaltd</code>.
          </p>
          <div className="mt-8 flex flex-wrap gap-3">
            {quickLinks.map((link) => (
              <a
                key={link.href}
                href={link.href}
                className="rounded-full border border-alpha bg-base-100 px-4 py-2 text-sm text-primary transition-colors duration-150 hover:border-alpha-strong hover:bg-base-50 dark:bg-surface"
              >
                {link.label}
              </a>
            ))}
          </div>
        </section>

        <section className="mt-12 grid gap-5 md:grid-cols-3 animate-fade-in-up [animation-delay:80ms]">
          <SummaryCard
            title="1. Install and point the CLI"
            copy="Install `gestalt`, save your server URL, and log in once."
          />
          <SummaryCard
            title="2. Connect what you need"
            copy="Use the web UI or CLI to authorize the integrations exposed by your team."
          />
          <SummaryCard
            title="3. Reuse the same access everywhere"
            copy="Invoke operations from the CLI, call the HTTP API directly, or attach an MCP client."
          />
        </section>

        <section
          id="install"
          className="mt-14 rounded-2xl border border-alpha bg-base-white/80 p-8 shadow-card backdrop-blur-sm dark:bg-surface/80"
        >
          <SectionHeading
            eyebrow="Install"
            title="Install the `gestalt` CLI"
            copy="You only need the client binary for day-to-day usage. `gestaltd` is the server your team runs."
          />
          <div className="mt-6 grid gap-5 lg:grid-cols-[1.2fr_0.8fr]">
            <CodeBlock
              title="Homebrew"
              code={`brew install valon-technologies/gestalt/gestalt`}
            />
            <Callout>
              Prefer direct downloads instead? Pull the binary from{" "}
              <a
                className="underline decoration-base-300 underline-offset-2 hover:text-primary dark:decoration-base-600"
                href="https://github.com/valon-technologies/gestalt/releases"
                target="_blank"
                rel="noreferrer"
              >
                GitHub releases
              </a>{" "}
              and place it on your <code className="font-mono text-sm">PATH</code>.
            </Callout>
          </div>
        </section>

        <section id="setup" className="mt-10">
          <SectionHeading
            eyebrow="Setup"
            title="Point the CLI at your Gestalt deployment"
            copy="Gestalt needs the base URL for your server, usually something like `https://gestalt.example.com`."
          />
          <div className="mt-6 grid gap-5 md:grid-cols-3">
            {setupWays.map((item) => (
              <div
                key={item.title}
                className="rounded-xl border border-alpha bg-base-100 p-6 dark:bg-surface"
              >
                <h3 className="text-lg font-medium text-primary">{item.title}</h3>
                <p className="mt-3 text-sm text-muted">{item.copy}</p>
                <pre className="mt-4 overflow-x-auto rounded-lg bg-base-950 px-4 py-3 font-mono text-sm text-base-100">
                  <code>{item.code}</code>
                </pre>
              </div>
            ))}
          </div>
          <p className="mt-4 text-sm text-muted">
            Resolution order is:{" "}
            <code className="font-mono text-sm text-primary">--url</code>,{" "}
            <code className="font-mono text-sm text-primary">GESTALT_URL</code>,{" "}
            <code className="font-mono text-sm text-primary">.gestalt.json</code>,
            your global CLI config, then{" "}
            <code className="font-mono text-sm text-primary">
              http://localhost:8080
            </code>
            .
          </p>
        </section>

        <section id="auth" className="mt-10">
          <SectionHeading
            eyebrow="Auth"
            title="Sign in or use an API token"
            copy="Browser login is the simplest path. For automation, use a `gst_api_...` token instead."
          />
          <div className="mt-6 grid gap-5 lg:grid-cols-2">
            <CodeBlock
              title="Interactive login"
              code={`gestalt auth login
gestalt auth status`}
            />
            <CodeBlock
              title="Token-based auth"
              code={`export GESTALT_API_KEY=gst_api_your_token_here
gestalt integrations list`}
            />
          </div>
          <p className="mt-4 text-sm text-muted">
            The browser login stores a scoped API token locally, so the CLI and
            the web UI share the same Gestalt deployment without asking you to
            paste credentials into each command.
          </p>
        </section>

        <section id="connect" className="mt-10">
          <SectionHeading
            eyebrow="Integrations"
            title="See what is available and connect accounts"
            copy="List integrations first, then start the connection flow for the ones you want to use."
          />
          <div className="mt-6 grid gap-5 lg:grid-cols-2">
            <CodeBlock
              title="List and connect"
              code={`gestalt integrations list
gestalt integrations connect <integration>
gestalt integrations connect <integration> --connection <name> --instance <instance>`}
            />
            <Callout>
              You can do the same work in the browser from{" "}
              <Link
                href="/integrations"
                className="underline decoration-base-300 underline-offset-2 hover:text-primary dark:decoration-base-600"
              >
                Integrations
              </Link>
              . Use the UI when you want to see connection state, token details,
              or integration descriptions before authorizing.
            </Callout>
          </div>
        </section>

        <section id="invoke" className="mt-10">
          <SectionHeading
            eyebrow="Invoke"
            title="Inspect operations, then call them"
            copy="Operation discovery is built into the CLI, so you can browse an integration before sending a real request."
          />
          <div className="mt-6 grid gap-5">
            <CodeBlock
              title="Common CLI flow"
              code={`gestalt invoke <integration>
gestalt describe <integration> <operation>
gestalt invoke <integration> <operation> -p key=value
gestalt invoke <integration> <operation> -p filters:='{"status":"open"}'
gestalt invoke <integration> <operation> --input-file payload.json --select data.items`}
            />
            <p className="text-sm text-muted">
              If you omit the operation,{" "}
              <code className="font-mono text-sm text-primary">
                gestalt invoke &lt;integration&gt;
              </code>{" "}
              lists the available operations instead of running one.
            </p>
          </div>
        </section>

        <section id="tokens" className="mt-10">
          <SectionHeading
            eyebrow="Tokens"
            title="Create tokens for scripts, CI, or remote clients"
            copy="User tokens are accepted by both the HTTP API and `/mcp`."
          />
          <div className="mt-6 grid gap-5 lg:grid-cols-2">
            <CodeBlock title="Token commands" code={tokenCommands.join("\n")} />
            <Callout>
              You can also mint and revoke tokens from{" "}
              <Link
                href="/tokens"
                className="underline decoration-base-300 underline-offset-2 hover:text-primary dark:decoration-base-600"
              >
                API Tokens
              </Link>
              . Gestalt only shows the raw token value once, so copy it into
              your secret manager immediately.
            </Callout>
          </div>
        </section>

        <section id="api" className="mt-10">
          <SectionHeading
            eyebrow="HTTP API"
            title="Call the same operations over HTTP"
            copy="Everything the CLI invokes is backed by the `gestaltd` HTTP API."
          />
          <div className="mt-6 grid gap-5">
            <CodeBlock
              title="List integrations"
              code={`curl \\
  -H "Authorization: Bearer $GESTALT_API_KEY" \\
  https://gestalt.example.com/api/v1/integrations`}
            />
            <CodeBlock
              title="Invoke one operation"
              code={`curl \\
  -H "Authorization: Bearer $GESTALT_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"example":"value"}' \\
  https://gestalt.example.com/api/v1/<integration>/<operation>`}
            />
          </div>
        </section>

        <section id="mcp" className="mt-10 mb-14">
          <SectionHeading
            eyebrow="MCP"
            title="Connect an MCP client to `gestaltd`"
            copy="Gestalt exposes a single authenticated MCP endpoint at `/mcp` whenever at least one integration enables MCP."
          />
          <div className="mt-6 grid gap-5 lg:grid-cols-[1.15fr_0.85fr]">
            <div className="space-y-5">
              <CodeBlock
                title="What most remote-capable clients need"
                code={`{
  "mcpServers": {
    "gestalt": {
      "url": "https://gestalt.example.com/mcp",
      "headers": {
        "Authorization": "Bearer gst_api_your_token_here"
      }
    }
  }
}`}
              />
              <p className="text-sm text-muted">
                Exact config keys vary by client, but the important pieces are
                stable: the endpoint URL is{" "}
                <code className="font-mono text-sm text-primary">/mcp</code>,
                and authentication uses the same bearer token format as the HTTP
                API.
              </p>
            </div>
            <div className="rounded-xl border border-alpha bg-base-100 p-6 dark:bg-surface">
              <h3 className="text-lg font-medium text-primary">
                MCP connection checklist
              </h3>
              <ul className="mt-4 space-y-3 text-sm text-muted">
                <li>
                  Use an MCP client that supports remote HTTP transport rather
                  than stdio-only servers.
                </li>
                <li>
                  Generate a user token with{" "}
                  <code className="font-mono text-sm text-primary">
                    gestalt tokens create
                  </code>{" "}
                  or from the UI.
                </li>
                <li>
                  Point the client at{" "}
                  <code className="font-mono text-sm text-primary">
                    https://your-host/mcp
                  </code>
                  .
                </li>
                <li>
                  Send{" "}
                  <code className="font-mono text-sm text-primary">
                    Authorization: Bearer gst_api_...
                  </code>
                  .
                </li>
                <li>
                  If the tool list is empty, check that the integration is both
                  MCP-enabled and already connected for your user.
                </li>
              </ul>
            </div>
          </div>
        </section>

        <section className="mb-16">
          <SectionHeading
            eyebrow="Troubleshooting"
            title="Common problems"
            copy="The failure modes are usually small: wrong URL, expired auth, or an ambiguous connection."
          />
          <div className="mt-6 grid gap-5 md:grid-cols-3">
            {issues.map((issue) => (
              <div
                key={issue.title}
                className="rounded-xl border border-alpha bg-base-100 p-6 dark:bg-surface"
              >
                <h3 className="text-lg font-medium text-primary">
                  {issue.title}
                </h3>
                <p className="mt-3 text-sm text-muted">{issue.copy}</p>
              </div>
            ))}
          </div>
        </section>
      </main>
    </div>
  );
}

function SummaryCard({ title, copy }: { title: string; copy: string }) {
  return (
    <div className="rounded-xl border border-alpha bg-base-100 p-6 shadow-card dark:bg-surface">
      <h2 className="text-lg font-medium text-primary">{title}</h2>
      <p className="mt-3 text-sm text-muted">{copy}</p>
    </div>
  );
}

function SectionHeading({
  eyebrow,
  title,
  copy,
}: {
  eyebrow: string;
  title: string;
  copy: string;
}) {
  return (
    <div>
      <span className="label-text">{eyebrow}</span>
      <h2 className="mt-2 text-2xl font-heading font-bold text-primary">
        {title}
      </h2>
      <p className="mt-3 max-w-3xl text-sm text-muted sm:text-base">{copy}</p>
    </div>
  );
}

function CodeBlock({ title, code }: { title: string; code: string }) {
  return (
    <div className="rounded-xl border border-alpha bg-base-100 p-6 dark:bg-surface">
      <h3 className="text-lg font-medium text-primary">{title}</h3>
      <pre className="mt-4 overflow-x-auto rounded-lg bg-base-950 px-4 py-4 font-mono text-sm text-base-100">
        <code>{code}</code>
      </pre>
    </div>
  );
}

function Callout({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-gold-200 bg-gold-50/80 p-6 text-sm text-secondary dark:border-gold-900 dark:bg-gold-950/30 dark:text-base-300">
      {children}
    </div>
  );
}
