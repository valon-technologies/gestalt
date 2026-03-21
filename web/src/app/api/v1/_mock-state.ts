export const connectedIntegrations = new Set<string>();

export interface MockToken {
  id: string;
  name: string;
  scopes: string;
  created_at: string;
  expires_at?: string;
}

let nextTokenId = 3;

export const mockTokens: MockToken[] = [
  { id: "tok-1", name: "ci-pipeline", scopes: "read", created_at: "2026-01-15T10:00:00Z" },
  { id: "tok-2", name: "deploy-key", scopes: "", created_at: "2026-02-20T14:30:00Z", expires_at: "2027-02-20T14:30:00Z" },
];

export function createMockToken(name: string, scopes: string): { id: string; token: string } {
  const id = `tok-${nextTokenId++}`;
  mockTokens.push({ id, name, scopes: scopes || "", created_at: new Date().toISOString() });
  return { id, token: `gestalt_mock_${id}` };
}

export function revokeMockToken(id: string): boolean {
  const idx = mockTokens.findIndex((t) => t.id === id);
  if (idx === -1) return false;
  mockTokens.splice(idx, 1);
  return true;
}
