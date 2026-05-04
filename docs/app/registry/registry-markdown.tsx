import type { ReactNode } from "react";
import { Fragment } from "react";

type LinkTarget = {
  href: string;
  external: boolean;
};

export type MarkdownRenderContext = {
  resolveLink: (href: string) => LinkTarget | null;
};

type Block =
  | { kind: "blockquote"; lines: string[] }
  | { kind: "code"; language: string; text: string }
  | { kind: "heading"; depth: number; text: string }
  | { kind: "list"; ordered: boolean; items: string[][] }
  | { kind: "paragraph"; text: string }
  | { kind: "table"; headers: string[]; rows: string[][] };

const headingPattern = /^(#{1,6})\s+(.+?)\s*#*$/;
const unorderedListPattern = /^(\s*)[-*]\s+(.+)$/;
const orderedListPattern = /^(\s*)\d+\.\s+(.+)$/;
const tableSeparatorPattern = /^\s*\|?\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)+\|?\s*$/;
const inlinePatternSource =
  /(`[^`]+`|\[([^\]]+)\]\(([^)]+)\)|\*\*([^*]+)\*\*|\*([^*]+)\*)/.source;

export function renderMarkdown(source: string, context: MarkdownRenderContext) {
  return parseBlocks(stripFrontmatter(source)).map((block, index) =>
    renderBlock(block, context, index),
  );
}

function stripFrontmatter(source: string) {
  const normalized = source.replace(/\r\n?/g, "\n");
  const lines = normalized.split("\n");
  if (lines[0]?.trim() !== "---") {
    return normalized;
  }
  const closeIndex = lines.slice(1).findIndex((line) => line.trim() === "---");
  if (closeIndex === -1) {
    return normalized;
  }
  return lines.slice(closeIndex + 2).join("\n");
}

function parseBlocks(source: string) {
  const blocks: Block[] = [];
  const lines = source.split("\n");
  let index = 0;
  while (index < lines.length) {
    const line = lines[index];
    const trimmed = line.trim();
    if (!trimmed) {
      index += 1;
      continue;
    }
    if (trimmed.startsWith("```") || trimmed.startsWith("~~~")) {
      const fence = trimmed.slice(0, 3);
      const language = trimmed.slice(3).trim();
      const codeLines: string[] = [];
      index += 1;
      while (index < lines.length && !lines[index].trim().startsWith(fence)) {
        codeLines.push(lines[index]);
        index += 1;
      }
      if (index < lines.length) {
        index += 1;
      }
      blocks.push({ kind: "code", language, text: codeLines.join("\n") });
      continue;
    }
    const heading = headingPattern.exec(trimmed);
    if (heading) {
      blocks.push({
        kind: "heading",
        depth: heading[1].length,
        text: heading[2].trim(),
      });
      index += 1;
      continue;
    }
    if (trimmed.startsWith(">")) {
      const quoteLines: string[] = [];
      while (index < lines.length && lines[index].trim().startsWith(">")) {
        quoteLines.push(lines[index].trim().replace(/^>\s?/, ""));
        index += 1;
      }
      blocks.push({ kind: "blockquote", lines: quoteLines });
      continue;
    }
    const unorderedListMatch = unorderedListPattern.exec(line);
    const orderedListMatch = orderedListPattern.exec(line);
    if (unorderedListMatch || orderedListMatch) {
      const ordered = orderedListMatch !== null;
      const markerIndent = (orderedListMatch ?? unorderedListMatch)?.[1].length ?? 0;
      const { items, nextIndex } = parseListItems(lines, index, ordered, markerIndent);
      blocks.push({ kind: "list", ordered, items });
      index = nextIndex;
      continue;
    }
    if (isTableStart(lines, index)) {
      const headers = parseTableRow(lines[index]);
      index += 2;
      const rows: string[][] = [];
      while (index < lines.length && lines[index].includes("|") && lines[index].trim()) {
        rows.push(parseTableRow(lines[index]));
        index += 1;
      }
      blocks.push({ kind: "table", headers, rows });
      continue;
    }

    const paragraphLines: string[] = [];
    while (index < lines.length && lines[index].trim() && !startsSpecialBlock(lines, index)) {
      paragraphLines.push(lines[index].trim());
      index += 1;
    }
    blocks.push({ kind: "paragraph", text: paragraphLines.join(" ") });
  }
  return blocks;
}

function parseListItems(
  lines: string[],
  startIndex: number,
  ordered: boolean,
  markerIndent: number,
) {
  const items: string[][] = [];
  let currentItem: string[] | null = null;
  let index = startIndex;
  while (index < lines.length) {
    const line = lines[index];
    if (!line.trim()) {
      break;
    }

    const listMatch = ordered
      ? orderedListPattern.exec(line)
      : unorderedListPattern.exec(line);
    if (listMatch && listMatch[1].length === markerIndent) {
      currentItem = [listMatch[2].trim()];
      items.push(currentItem);
      index += 1;
      continue;
    }

    if (
      currentItem &&
      lineIndent(line) > markerIndent &&
      !isBlockBoundary(lines, index)
    ) {
      currentItem.push(line.trim());
      index += 1;
      continue;
    }

    break;
  }
  return { items, nextIndex: index };
}

function lineIndent(line: string) {
  return line.length - line.trimStart().length;
}

function isBlockBoundary(lines: string[], index: number) {
  const trimmed = lines[index].trim();
  return (
    trimmed.startsWith("```") ||
    trimmed.startsWith("~~~") ||
    headingPattern.test(trimmed) ||
    isTableStart(lines, index)
  );
}

function startsSpecialBlock(lines: string[], index: number) {
  const trimmed = lines[index].trim();
  return (
    trimmed.startsWith("```") ||
    trimmed.startsWith("~~~") ||
    trimmed.startsWith(">") ||
    headingPattern.test(trimmed) ||
    unorderedListPattern.test(lines[index]) ||
    orderedListPattern.test(lines[index]) ||
    isTableStart(lines, index)
  );
}

function isTableStart(lines: string[], index: number) {
  return (
    index + 1 < lines.length &&
    lines[index].includes("|") &&
    tableSeparatorPattern.test(lines[index + 1])
  );
}

function parseTableRow(line: string) {
  return line
    .trim()
    .replace(/^\|/, "")
    .replace(/\|$/, "")
    .split("|")
    .map((cell) => cell.trim());
}

function renderBlock(block: Block, context: MarkdownRenderContext, index: number) {
  switch (block.kind) {
    case "blockquote":
      return (
        <blockquote key={index}>
          {block.lines.map((line, lineIndex) => (
            <Fragment key={lineIndex}>
              {lineIndex > 0 ? <br /> : null}
              {renderInline(line, context)}
            </Fragment>
          ))}
        </blockquote>
      );
    case "code":
      return (
        <pre key={index}>
          <code data-language={block.language || undefined}>{block.text}</code>
        </pre>
      );
    case "heading": {
      const depth = Math.min(block.depth + 1, 4);
      const Heading = `h${depth}` as "h2" | "h3" | "h4";
      return <Heading key={index}>{renderInline(block.text, context)}</Heading>;
    }
    case "list": {
      const List = block.ordered ? "ol" : "ul";
      return (
        <List key={index}>
          {block.items.map((item, itemIndex) => (
            <li key={itemIndex}>
              {item.map((line, lineIndex) => (
                <Fragment key={lineIndex}>
                  {lineIndex > 0 ? <br /> : null}
                  {renderInline(line, context)}
                </Fragment>
              ))}
            </li>
          ))}
        </List>
      );
    }
    case "paragraph":
      return <p key={index}>{renderInline(block.text, context)}</p>;
    case "table":
      return (
        <div key={index} className="registry-markdown-table">
          <table>
            <thead>
              <tr>
                {block.headers.map((header, headerIndex) => (
                  <th key={headerIndex}>{renderInline(header, context)}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {block.rows.map((row, rowIndex) => (
                <tr key={rowIndex}>
                  {row.map((cell, cellIndex) => (
                    <td key={cellIndex}>{renderInline(cell, context)}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
  }
}

function renderInline(text: string, context: MarkdownRenderContext): ReactNode[] {
  const nodes: ReactNode[] = [];
  let cursor = 0;
  let match: RegExpExecArray | null;
  const inlinePattern = new RegExp(inlinePatternSource, "g");
  while ((match = inlinePattern.exec(text)) !== null) {
    if (match.index > cursor) {
      nodes.push(text.slice(cursor, match.index));
    }
    const token = match[0];
    const key = `${match.index}-${token}`;
    if (token.startsWith("`")) {
      nodes.push(<code key={key}>{token.slice(1, -1)}</code>);
    } else if (match[2] && match[3]) {
      const target = context.resolveLink(match[3]);
      if (target) {
        nodes.push(
          <a
            href={target.href}
            key={key}
            rel={target.external ? "noreferrer" : undefined}
            target={target.external ? "_blank" : undefined}
          >
            {renderInline(match[2], context)}
          </a>,
        );
      } else {
        nodes.push(match[2]);
      }
    } else if (match[4]) {
      nodes.push(<strong key={key}>{renderInline(match[4], context)}</strong>);
    } else if (match[5]) {
      nodes.push(<em key={key}>{renderInline(match[5], context)}</em>);
    }
    cursor = match.index + token.length;
  }
  if (cursor < text.length) {
    nodes.push(text.slice(cursor));
  }
  return nodes;
}
