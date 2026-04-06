"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import Nav from "@/components/Nav";

const FALLBACK_ORIGIN = "https://your-gestalt-host";

const sections = [
  { id: "overview", label: "Overview" },
  { id: "setup", label: "Set Up The CLI" },
  { id: "connect", label: "Connect Integrations" },
  { id: "invoke", label: "Invoke Operations" },
  { id: "tokens", label: "Manage API Tokens" },
  { id: "mcp", label: "Use With MCP" },
  { id: "troubleshooting", label: "Troubleshooting" },
];

export default function DocsClient() {
  const origin = useDeploymentOrigin();

  return (
    <div className="min-h-screen">
      <Nav />
      <main className="mx-auto max-w-[1400px] px-6 py-10">
        <div className="grid gap-10 xl:grid-cols-[220px_minmax(0,1fr)_240px]">
          <aside className="hidden xl:block">
            <div className="sticky top-24 rounded-xl border border-alpha bg-base-white/80 p-5 dark:bg-surface/80">
              <p className="text-xs font-medium uppercase tracking-[0.16em] text-faint">
                Docs
              </p>
              <nav className="mt-4 space-y-1">
                {sections.map((section) => (
                  <a
                    key={section.id}
                    href={`#${section.id}`}
                    className="block rounded-md px-3 py-2 text-sm text-muted transition-colors duration-150 hover:bg-alpha-5 hover:text-primary"
                  >
                    {section.label}
                  </a>
                ))}
              </nav>
            </div>
          </aside>

          <article className="min-w-0">
            <header
              id="overview"
              className="border-b border-alpha pb-8 animate-fade-in-up"
            >
              <p className="text-sm text-muted">Overview</p>
              <h1 className="mt-3 font-heading text-4xl font-bold tracking-[-0.03em] text-primary sm:text-5xl">
                Gestalt User Guide
              </h1>
              <p className="mt-5 max-w-3xl text-base leading-7 text-secondary">
                This page covers the user-facing workflows for the Gestalt
                deployment you are currently using: install the{" "}
                <code className="font-mono text-sm text-primary">gestalt</code>{" "}
                CLI, point it at this deployment, sign in, connect
                integrations, invoke operations, mint API tokens, and attach an
                MCP-aware client to the same server.
              </p>
              <div className="mt-6 rounded-xl border border-alpha bg-base-100 p-4 dark:bg-surface">
                <p className="text-xs font-medium uppercase tracking-[0.16em] text-faint">
                  Deployment URL
                </p>
                <p className="mt-2 font-mono text-sm text-primary">{origin}</p>
                <p className="mt-2 text-sm leading-6 text-muted">
                  Full URLs on this page use the current deployment origin so
                  you can copy commands without replacing{" "}
                  <code className="font-mono text-sm">gestalt.example.com</code>{" "}
                  by hand.
                </p>
              </div>
            </header>

            <DocSection
              id="setup"
              title="Set Up The CLI"
              description="Install the client binary, point it at this deployment, and authenticate once."
            >
              <Subheading title="Install" />
              <p className="doc-copy">
                End users only need the{" "}
                <code className="font-mono text-sm text-primary">gestalt</code>{" "}
                CLI. <code className="font-mono text-sm text-primary">gestaltd</code>{" "}
                is the server binary used by whoever operates the deployment.
              </p>
              <CodeBlock
                code={`brew install valon-technologies/gestalt/gestalt`}
              />
              <p className="doc-copy">
                If you prefer a direct download, use the{" "}
                <a
                  href="https://github.com/valon-technologies/gestalt/releases"
                  target="_blank"
                  rel="noreferrer"
                  className="doc-link"
                >
                  GitHub releases page
                </a>{" "}
                and place the binary on your{" "}
                <code className="font-mono text-sm text-primary">PATH</code>.
              </p>

              <Subheading title="Point the CLI at this deployment" />
              <p className="doc-copy">
                The CLI needs the base URL for the Gestalt server. Use either
                the setup wizard or a direct config command.
              </p>
              <CodeBlock
                code={`gestalt init
gestalt config set url ${origin}
export GESTALT_URL=${origin}`}
              />
              <InfoTable
                rows={[
                  [
                    "gestalt init",
                    "Interactive setup that stores the URL and can start browser login.",
                  ],
                  [
                    "gestalt config set url ...",
                    "Persistent global config for your user account on this machine.",
                  ],
                  [
                    "GESTALT_URL",
                    "Per-shell override when you do not want to change stored config.",
                  ],
                ]}
              />
              <p className="doc-copy">
                Resolution order is{" "}
                <code className="font-mono text-sm text-primary">--url</code>,{" "}
                <code className="font-mono text-sm text-primary">GESTALT_URL</code>,
                project-local{" "}
                <code className="font-mono text-sm text-primary">.gestalt.json</code>,
                global CLI config, then{" "}
                <code className="font-mono text-sm text-primary">
                  http://localhost:8080
                </code>
                .
              </p>

              <Subheading title="Authenticate" />
              <p className="doc-copy">
                Browser login is the normal path for interactive use. For
                scripts, you can also set a Gestalt API token directly.
              </p>
              <CodeBlock
                code={`gestalt auth login
gestalt auth status

export GESTALT_API_KEY=gst_api_your_token_here
gestalt integrations list`}
              />
            </DocSection>

            <DocSection
              id="connect"
              title="Connect Integrations"
              description="Inspect available integrations first, then authorize the ones you need."
            >
              <p className="doc-copy">
                Integrations exposed by the deployment appear in both the CLI
                and the web UI. Use either surface to start the underlying OAuth
                or manual credential flow.
              </p>
              <CodeBlock
                code={`gestalt integrations list
gestalt integrations connect <integration>
gestalt integrations connect <integration> --connection <name> --instance <instance>`}
              />
              <p className="doc-copy">
                If you prefer the browser flow, the same work is available on{" "}
                <Link href="/integrations" className="doc-link">
                  Integrations
                </Link>
                .
              </p>
            </DocSection>

            <DocSection
              id="invoke"
              title="Invoke Operations"
              description="Use the catalog built into Gestalt to discover an integration's operations before making requests."
            >
              <CodeBlock
                code={`gestalt invoke <integration>
gestalt describe <integration> <operation>
gestalt invoke <integration> <operation> -p key=value
gestalt invoke <integration> <operation> -p filters:='{"status":"open"}'
gestalt invoke <integration> <operation> --input-file payload.json --select data.items`}
              />
              <p className="doc-copy">
                If you omit the operation,{" "}
                <code className="font-mono text-sm text-primary">
                  gestalt invoke &lt;integration&gt;
                </code>{" "}
                lists available operations instead of running one.
              </p>

              <Subheading title="Invoke over HTTP" />
              <p className="doc-copy">
                The CLI calls the same HTTP API that the server exposes for
                direct programmatic access.
              </p>
              <CodeBlock
                code={`curl \\
  -H "Authorization: Bearer $GESTALT_API_KEY" \\
  ${origin}/api/v1/integrations

curl \\
  -H "Authorization: Bearer $GESTALT_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"example":"value"}' \\
  ${origin}/api/v1/<integration>/<operation>`}
              />
            </DocSection>

            <DocSection
              id="tokens"
              title="Manage API Tokens"
              description="User tokens work for both the HTTP API and the MCP endpoint."
            >
              <CodeBlock
                code={`gestalt tokens create --name automation
gestalt tokens list
gestalt tokens revoke <token-id>`}
              />
              <p className="doc-copy">
                Tokens can also be created from{" "}
                <Link href="/tokens" className="doc-link">
                  API Tokens
                </Link>
                . The raw token value is shown once, so store it immediately in
                your secret manager or shell environment.
              </p>
            </DocSection>

            <DocSection
              id="mcp"
              title="Use With MCP"
              description="Gestalt exposes one authenticated MCP endpoint at `/mcp` whenever at least one integration enables MCP."
            >
              <p className="doc-copy">
                The endpoint for this deployment is{" "}
                <code className="font-mono text-sm text-primary">
                  {origin}/mcp
                </code>
                .
              </p>
              <CodeBlock
                code={`{
  "mcpServers": {
    "gestalt": {
      "url": "${origin}/mcp",
      "headers": {
        "Authorization": "Bearer gst_api_your_token_here"
      }
    }
  }
}`}
              />
              <p className="doc-copy">
                Exact client config keys vary, but the important parts do not:
                use the deployed{" "}
                <code className="font-mono text-sm text-primary">/mcp</code>{" "}
                URL and send the same bearer token format accepted by the HTTP
                API.
              </p>
              <InfoTable
                rows={[
                  ["Endpoint", `${origin}/mcp`],
                  ["Authentication", "Authorization: Bearer gst_api_..."],
                  [
                    "If no tools appear",
                    "Confirm that the integration is MCP-enabled and connected for your user.",
                  ],
                ]}
              />
            </DocSection>

            <DocSection
              id="troubleshooting"
              title="Troubleshooting"
              description="Most user-facing problems come down to the wrong URL, expired auth, or ambiguous connection selection."
            >
              <Subheading title="The CLI says you are not authenticated" />
              <p className="doc-copy">
                Run{" "}
                <code className="font-mono text-sm text-primary">
                  gestalt auth login
                </code>
                , or set{" "}
                <code className="font-mono text-sm text-primary">
                  GESTALT_API_KEY
                </code>{" "}
                if you are using a token directly.
              </p>

              <Subheading title="An integration has multiple connections" />
              <p className="doc-copy">
                Pass{" "}
                <code className="font-mono text-sm text-primary">
                  --connection
                </code>{" "}
                or{" "}
                <code className="font-mono text-sm text-primary">
                  --instance
                </code>{" "}
                so Gestalt can resolve the correct credentials.
              </p>

              <Subheading title="The MCP endpoint is mounted, but the tool list is empty" />
              <p className="doc-copy">
                That usually means the integration is available in the server
                config but has not been connected for your current user yet.
              </p>
            </DocSection>
          </article>

          <aside className="hidden xl:block">
            <div className="sticky top-24 space-y-5">
              <div className="rounded-xl border border-alpha bg-base-white/80 p-5 dark:bg-surface/80">
                <p className="text-xs font-medium uppercase tracking-[0.16em] text-faint">
                  On This Page
                </p>
                <nav className="mt-4 space-y-1">
                  {sections.map((section) => (
                    <a
                      key={section.id}
                      href={`#${section.id}`}
                      className="block rounded-md px-3 py-2 text-sm text-muted transition-colors duration-150 hover:bg-alpha-5 hover:text-primary"
                    >
                      {section.label}
                    </a>
                  ))}
                </nav>
              </div>
              <div className="rounded-xl border border-alpha bg-base-white/80 p-5 text-sm leading-6 text-muted dark:bg-surface/80">
                <p className="text-xs font-medium uppercase tracking-[0.16em] text-faint">
                  Current Host
                </p>
                <p className="mt-3 break-all font-mono text-xs text-primary">
                  {origin}
                </p>
              </div>
            </div>
          </aside>
        </div>
      </main>
    </div>
  );
}

function useDeploymentOrigin() {
  const [origin, setOrigin] = useState(FALLBACK_ORIGIN);

  useEffect(() => {
    setOrigin(window.location.origin);
  }, []);

  return origin;
}

function DocSection({
  id,
  title,
  description,
  children,
}: {
  id: string;
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <section id={id} className="border-b border-alpha py-10">
      <h2 className="text-3xl font-heading font-bold tracking-[-0.02em] text-primary">
        {title}
      </h2>
      <p className="mt-3 max-w-3xl text-base leading-7 text-muted">
        {description}
      </p>
      <div className="mt-6 space-y-5">{children}</div>
    </section>
  );
}

function Subheading({ title }: { title: string }) {
  return (
    <h3 className="pt-2 text-lg font-semibold tracking-[-0.01em] text-primary">
      {title}
    </h3>
  );
}

function CodeBlock({ code }: { code: string }) {
  return (
    <pre className="overflow-x-auto rounded-xl border border-alpha bg-base-100 px-4 py-4 font-mono text-sm leading-6 text-primary dark:bg-surface">
      <code>{code}</code>
    </pre>
  );
}

function InfoTable({ rows }: { rows: [string, string][] }) {
  return (
    <div className="overflow-hidden rounded-xl border border-alpha">
      <table className="w-full border-collapse bg-base-white text-left text-sm dark:bg-surface">
        <tbody>
          {rows.map(([label, value]) => (
            <tr key={label} className="border-t border-alpha first:border-t-0">
              <th className="w-56 bg-base-100 px-4 py-3 align-top font-medium text-primary dark:bg-surface-raised">
                {label}
              </th>
              <td className="px-4 py-3 text-muted">{value}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
