export type AdminConfig = {
  brandHref: string;
  loginBase: string;
};

declare global {
  interface Window {
    __GESTALT_ADMIN__?: Partial<AdminConfig>;
  }
}

export function adminConfig(): AdminConfig {
  const fromWindow = window.__GESTALT_ADMIN__;
  return {
    brandHref: fromWindow?.brandHref?.trim() || "/",
    loginBase: fromWindow?.loginBase?.trim() || "/api/v1/auth/login",
  };
}
