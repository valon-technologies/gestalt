import type { ReactNode } from "react";
import { Callout } from "nextra/components";
import { Link } from "nextra-theme-docs";

const DEFAULT_CHANGE_NOTE =
  "Breaking changes may happen between releases without warning.";
const ISSUES_URL = "https://github.com/valon-technologies/gestalt/issues";

type ActiveDevelopmentCalloutProps = {
  children: ReactNode;
  changeNote?: ReactNode;
  verb?: "is" | "are";
};

export default function ActiveDevelopmentCallout({
  children,
  changeNote = DEFAULT_CHANGE_NOTE,
  verb = "is",
}: ActiveDevelopmentCalloutProps) {
  const stabilityVerb = verb === "are" ? "are" : "is";

  return (
    <Callout type="warning">
      {children} {verb} under active development and {stabilityVerb} not yet
      stable. {changeNote} Feedback and bug reports are welcome via{" "}
      <Link href={ISSUES_URL}>GitHub Issues</Link>.
    </Callout>
  );
}
