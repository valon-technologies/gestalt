"use client";

import { useEffect, useState } from "react";

// Vitesse Light/Dark, matching the docs MDX pipeline. Backgrounds left to the
// page surface and the light comment color lifted to an AA-passing warm
// brown (vitesse's #a0ada0 is 2.3:1 on our cream).
const themes = { light: "vitesse-light", dark: "vitesse-dark" } as const;
const colorReplacements = {
  "vitesse-light": { "#a0ada0": "#76705f" },
} as const;

const htmlCache = new Map<string, Promise<string | null>>();

function highlight(language: string, text: string) {
  const key = `${language}\u0000${text}`;
  const cached = htmlCache.get(key);
  if (cached) {
    return cached;
  }
  const promise = import("shiki")
    .then((shiki) =>
      shiki.codeToHtml(text, {
        lang: language || "text",
        themes,
        defaultColor: false,
        colorReplacements,
      }),
    )
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
