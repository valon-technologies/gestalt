"use client";

import { useEffect, useState } from "react";
import type { HighlighterCore } from "shiki/core";

// Vitesse Light/Dark, matching the docs MDX pipeline. Backgrounds left to the
// page surface and the light comment color lifted to an AA-passing warm
// brown (vitesse's #a0ada0 is 2.3:1 on our cream).
const themes = { light: "vitesse-light", dark: "vitesse-dark" } as const;
const colorReplacements = {
  "vitesse-light": { "#a0ada0": "#76705f" },
} as const;

// A curated core bundle instead of `import("shiki")`: the full bundle
// code-splits every one of its ~300 grammars into the static export, and
// the proto grammar's literal "protobuf" strings even trip the SDK-doc
// leak check over the exported JS. These are the languages provider docs
// actually use; anything else renders as plain text via highlight().
let highlighterPromise: Promise<HighlighterCore> | null = null;

function getHighlighter() {
  highlighterPromise ??= Promise.all([
    import("shiki/core"),
    import("shiki/engine/javascript"),
  ]).then(([core, engine]) =>
    core.createHighlighterCore({
      themes: [
        import("@shikijs/themes/vitesse-light"),
        import("@shikijs/themes/vitesse-dark"),
      ],
      langs: [
        import("@shikijs/langs/shellscript"),
        import("@shikijs/langs/yaml"),
        import("@shikijs/langs/json"),
        import("@shikijs/langs/typescript"),
        import("@shikijs/langs/javascript"),
        import("@shikijs/langs/go"),
      ],
      engine: engine.createJavaScriptRegexEngine({ forgiving: true }),
    }),
  );
  return highlighterPromise;
}

const htmlCache = new Map<string, Promise<string | null>>();

function highlight(language: string, text: string) {
  const key = `${language}\u0000${text}`;
  const cached = htmlCache.get(key);
  if (cached) {
    return cached;
  }
  const promise = getHighlighter()
    .then((highlighter) => {
      const lang =
        language && highlighter.getLoadedLanguages().includes(language)
          ? language
          : "text";
      return highlighter.codeToHtml(text, {
        lang,
        themes,
        defaultColor: false,
        colorReplacements,
      });
    })
    .catch(() => null);
  htmlCache.set(key, promise);
  return promise;
}

export default function ShikiCode({
  language,
  text,
}: {
  language: string;
  text: string;
}) {
  const [html, setHtml] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    void highlight(language, text).then((result) => {
      if (!cancelled) {
        setHtml(result);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [language, text]);

  if (!html) {
    return (
      <pre>
        <code data-language={language || undefined}>{text}</code>
      </pre>
    );
  }
  // Shiki escapes all code content; this is its own generated markup.
  // eslint-disable-next-line react/no-danger
  return <div dangerouslySetInnerHTML={{ __html: html }} />;
}
