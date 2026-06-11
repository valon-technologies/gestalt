import { Footer } from "nextra-theme-docs";
import ThemeSelect from "./theme-select";

export default function ShellFooter() {
  return (
    <Footer className="shell-footer">
      <ThemeSelect />
    </Footer>
  );
}
