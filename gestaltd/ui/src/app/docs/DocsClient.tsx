"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import Nav from "@/components/Nav";
import { CopyIcon, CheckIcon } from "@/components/icons";

const FALLBACK_ORIGIN = "https://your-gestalt-host";

interface Subsection {
  id: string;
  label: string;
}

interface Section {
  id: string;
  label: string;
  subsections: Subsection[];
}

const sections: Section[] = [
  { id: "overview", label: "Overview", subsections: [] },
  {
    id: "setup",
    label: "Set Up The CLI",
    subsections: [
      { id: "install", label: "Install" },
      { id: "point-cli", label: "Point the CLI" },
      { id: "authenticate", label: "Authenticate" },
    ],
  },
  { id: "connect", label: "Connect Integrations", subsections: [] },
  {
    id: "invoke",
    label: "Invoke Operations",
    subsections: [{ id: "invoke-http", label: "Invoke over HTTP" }],
  },
  { id: "tokens", label: "Manage API Tokens", subsections: [] },
  {
    id: "mcp",
    label: "Use With MCP",
    subsections: [
      { id: "mcp-claude-code", label: "Claude Code" },
      { id: "mcp-claude-desktop", label: "Claude Desktop" },
      { id: "mcp-cursor", label: "Cursor" },
      { id: "mcp-vscode-copilot", label: "VS Code Copilot" },
      { id: "mcp-windsurf", label: "Windsurf" },
      { id: "mcp-other", label: "Other Clients" },
    ],
  },
  {
    id: "troubleshooting",
    label: "Troubleshooting",
    subsections: [
      { id: "ts-not-authenticated", label: "Not authenticated" },
      { id: "ts-multiple-connections", label: "Multiple connections" },
      { id: "ts-empty-tools", label: "Empty MCP tool list" },
    ],
  },
];

const allTrackableIds = sections.flatMap((s) => [
  s.id,
  ...s.subsections.map((sub) => sub.id),
]);

function useScrollSpy(ids: string[], offset = 100) {
  const [activeId, setActiveId] = useState(ids[0] ?? "");

  useEffect(() => {
    function onScroll() {
      const atBottom =
        window.innerHeight + window.scrollY >=
        document.documentElement.scrollHeight - 40;
      if (atBottom) {
        setActiveId(ids[ids.length - 1] ?? "");
        return;
      }
      let current = ids[0] ?? "";
      for (const id of ids) {
        const el = document.getElementById(id);
        if (el && el.getBoundingClientRect().top <= offset) {
          current = id;
        }
      }
      setActiveId(current);
    }
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, [ids, offset]);

  return activeId;
}

export default function DocsClient() {
  const origin = useDeploymentOrigin();
  const activeId = useScrollSpy(allTrackableIds);

  const activeSection = sections.find(
    (s) =>
      s.id === activeId ||
      s.subsections.some((sub) => sub.id === activeId),
  );
  const activeSectionId = activeSection?.id ?? sections[0].id;
  const activeSubsections = activeSection?.subsections ?? [];

  return (
    <div className="min-h-screen">
      <Nav />
      <main className="mx-auto max-w-[1400px] px-6 py-16">
        <div className="grid gap-10 xl:grid-cols-[220px_minmax(0,1fr)_240px]">
          <aside className="hidden xl:block">
            <div className="sticky top-24">
              <nav className="space-y-0.5">
                {sections.map((section) => {
                  const isActive = section.id === activeSectionId;
                  return (
                    <a
                      key={section.id}
                      href={`#${section.id}`}
                      className={`block rounded-md px-3 py-2 text-sm transition-colors duration-150 ${
                        isActive
                          ? "bg-alpha-5 font-medium text-primary"
                          : "text-muted hover:text-primary"
                      }`}
                    >
                      {section.label}
                    </a>
                  );
                })}
              </nav>
            </div>
          </aside>

          <article className="min-w-0">
            <header
              id="overview"
              className="scroll-mt-24 border-b border-alpha pb-10 animate-fade-in-up"
            >
              <p className="text-xs font-medium uppercase tracking-[0.16em] text-faint">
                Overview
              </p>
              <h1 className="mt-5 font-heading text-4xl font-bold tracking-[-0.03em] text-primary sm:text-5xl">
                Gestalt User Guide
              </h1>
              <p className="mt-6 max-w-3xl text-base leading-7 text-secondary">
                This page covers the user-facing workflows for the Gestalt
                deployment you are currently using: install the{" "}
                <code className="font-mono text-sm text-primary">gestalt</code>{" "}
                CLI, point it at this deployment, sign in, connect
                integrations, invoke operations, mint API tokens, and attach an
                MCP-aware client to the same server. No command-line
                experience is required — follow the steps below and copy the
                commands as-is.
              </p>
              <div className="mt-8 rounded-xl border border-alpha bg-base-100 p-5 dark:bg-surface">
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
              <Subheading id="install" title="Install" />
              <p className="doc-copy">
                End users only need the{" "}
                <code className="font-mono text-sm text-primary">gestalt</code>{" "}
                CLI. <code className="font-mono text-sm text-primary">gestaltd</code>{" "}
                is the server binary used by whoever operates the deployment.
              </p>
              <p className="doc-copy">
                The recommended way to install is with{" "}
                <a href="https://brew.sh" target="_blank" rel="noreferrer" className="doc-link">Homebrew</a>
                , a free package manager for macOS and Linux.
                If you do not have Homebrew yet, visit{" "}
                <a href="https://brew.sh" target="_blank" rel="noreferrer" className="doc-link">brew.sh</a>
                {" "}to install it first.
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

              <Subheading id="point-cli" title="Point the CLI at this deployment" />
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
                    "Interactive setup that stores the URL, can create a project-local .gestalt/config.json, and can start browser login.",
                  ],
                  [
                    "gestalt config set url ...",
                    "Writes the user-local CLI config file for this machine.",
                  ],
                  [
                    ".gestalt/config.json",
                    "Project-local URL override for one checkout or deployment directory.",
                  ],
                  [
                    "GESTALT_URL",
                    "Per-shell override when you do not want to change stored config.",
                  ],
                ]}
              />
              <p className="doc-copy">
                The optional{" "}
                <code className="font-mono text-sm text-primary">
                  .gestalt/config.json
                </code>{" "}
                file stores only the deployment URL. The CLI searches the
                current directory and then parent directories until it finds the
                nearest project config.
              </p>
              <p className="doc-copy">
                Resolution order is{" "}
                <code className="font-mono text-sm text-primary">--url</code>,{" "}
                <code className="font-mono text-sm text-primary">GESTALT_URL</code>,
                project-local{" "}
                <code className="font-mono text-sm text-primary">
                  .gestalt/config.json
                </code>,{" "}
                user-local CLI config file, for example{" "}
                <code className="font-mono text-sm text-primary">
                  ~/.config/gestalt/config.json
                </code>
                .
              </p>

              <Subheading id="authenticate" title="Authenticate" />
              <p className="doc-copy">
                Browser login is the normal path for interactive use. Running
                the command below opens your browser automatically — just
                approve the sign-in when prompted. For scripts, you can also
                set a Gestalt API token directly.
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

              <Subheading id="invoke-http" title="Invoke over HTTP" />
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
              description="Gestalt exposes a single MCP endpoint that gives AI tools access to all your connected integrations. Create an API token on the API Tokens page first, then configure your preferred tool below."
            >
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

              <Subheading id="mcp-claude-code" title="Claude Code" />
              <p className="doc-copy">
                Add Gestalt as an MCP server in your Claude Code settings. You
                can configure it globally or per-project.
              </p>
              <p className="doc-copy">
                Config file:{" "}
                <code className="font-mono text-sm text-primary">
                  .claude/settings.json
                </code>{" "}
                (global) or{" "}
                <code className="font-mono text-sm text-primary">
                  .claude/settings.local.json
                </code>{" "}
                (project)
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
                You can also run{" "}
                <code className="font-mono text-sm text-primary">
                  gestalt init
                </code>{" "}
                to configure this automatically.
              </p>

              <Subheading id="mcp-claude-desktop" title="Claude Desktop" />
              <p className="doc-copy">
                Add the same server block to Claude Desktop&apos;s config file:
              </p>
              <InfoTable
                rows={[
                  [
                    "macOS",
                    "~/Library/Application Support/Claude/claude_desktop_config.json",
                  ],
                  [
                    "Windows",
                    "%APPDATA%\\Claude\\claude_desktop_config.json",
                  ],
                ]}
              />
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
                Restart Claude Desktop after editing the config file.
              </p>

              <Subheading id="mcp-cursor" title="Cursor" />
              <p className="doc-copy">
                Config file:{" "}
                <code className="font-mono text-sm text-primary">
                  .cursor/mcp.json
                </code>{" "}
                in your project root, or{" "}
                <code className="font-mono text-sm text-primary">
                  ~/.cursor/mcp.json
                </code>{" "}
                globally.
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

              <Subheading id="mcp-vscode-copilot" title="VS Code Copilot" />
              <p className="doc-copy">
                Config file:{" "}
                <code className="font-mono text-sm text-primary">
                  .vscode/settings.json
                </code>
              </p>
              <CodeBlock
                code={`{
  "github.copilot.chat.mcp.servers": [
    {
      "name": "gestalt",
      "type": "http",
      "url": "${origin}/mcp",
      "headers": {
        "Authorization": "Bearer gst_api_your_token_here"
      }
    }
  ]
}`}
              />

              <Subheading id="mcp-windsurf" title="Windsurf" />
              <p className="doc-copy">
                Config file:{" "}
                <code className="font-mono text-sm text-primary">
                  ~/.codeium/windsurf/mcp_config.json
                </code>
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

              <Subheading id="mcp-other" title="Other Clients" />
              <p className="doc-copy">
                Any MCP-compatible client can connect to Gestalt. You need
                three pieces of information:
              </p>
              <InfoTable
                rows={[
                  ["URL", `${origin}/mcp`],
                  ["Header", "Authorization: Bearer gst_api_..."],
                  ["Config key", "usually mcpServers"],
                ]}
              />
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
            </DocSection>

            <DocSection
              id="troubleshooting"
              title="Troubleshooting"
              description="Most user-facing problems come down to the wrong URL, expired auth, or ambiguous connection selection."
            >
              <Subheading id="ts-not-authenticated" title="The CLI says you are not authenticated" />
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

              <Subheading id="ts-multiple-connections" title="An integration has multiple connections" />
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

              <Subheading id="ts-empty-tools" title="The MCP endpoint is mounted, but the tool list is empty" />
              <p className="doc-copy">
                That usually means the integration is available in the server
                config but has not been connected for your current user yet.
              </p>
            </DocSection>
          </article>

          <aside className="hidden xl:block">
            <div className="sticky top-24 space-y-6">
              {activeSubsections.length > 0 && (
                <div>
                  <p className="text-xs font-medium uppercase tracking-[0.16em] text-faint">
                    On This Page
                  </p>
                  <nav className="mt-3 space-y-0.5">
                    {activeSubsections.map((sub) => (
                      <a
                        key={sub.id}
                        href={`#${sub.id}`}
                        className={`block border-l-2 py-1.5 pl-3 text-sm transition-colors duration-150 ${
                          sub.id === activeId
                            ? "border-gold-600 text-primary dark:border-gold-300"
                            : "border-transparent text-muted hover:border-base-300 hover:text-primary dark:hover:border-base-600"
                        }`}
                      >
                        {sub.label}
                      </a>
                    ))}
                  </nav>
                </div>
              )}
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
  const [origin] = useState(() =>
    typeof window === "undefined" ? FALLBACK_ORIGIN : window.location.origin,
  );
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
    <section id={id} className="scroll-mt-24 border-b border-alpha py-12">
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

function Subheading({ id, title }: { id?: string; title: string }) {
  return (
    <h3
      id={id}
      className="scroll-mt-24 pt-2 text-lg font-semibold tracking-[-0.01em] text-primary"
    >
      {title}
    </h3>
  );
}

function CodeBlock({ code }: { code: string }) {
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    try {
      await navigator.clipboard.writeText(code);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {}
  }

  return (
    <div className="relative group">
      <pre className="overflow-x-auto rounded-xl border border-alpha bg-base-100 px-4 py-4 pr-12 font-mono text-sm leading-6 text-primary dark:bg-surface">
        <code>{code}</code>
      </pre>
      <button
        onClick={handleCopy}
        className="absolute right-3 top-3 rounded-md p-1.5 text-muted opacity-0 transition-all duration-150 hover:bg-alpha-5 hover:text-primary group-hover:opacity-100"
        title="Copy to clipboard"
        aria-label="Copy to clipboard"
      >
        {copied ? (
          <CheckIcon className="h-4 w-4 text-grove-600 dark:text-grove-200" />
        ) : (
          <CopyIcon className="h-4 w-4" />
        )}
      </button>
    </div>
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
