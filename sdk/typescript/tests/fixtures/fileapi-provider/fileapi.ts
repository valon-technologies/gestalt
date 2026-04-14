import { createMemoryFileAPIProvider } from "../../fileapi_memory.ts";

export const provider = createMemoryFileAPIProvider({
  displayName: "Fixture FileAPI",
  description: "Fixture FileAPI provider",
});
