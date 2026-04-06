"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { isAuthenticated } from "@/lib/auth";
import { LOGIN_PATH } from "@/lib/constants";

export default function AuthGuard({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const authenticated = isAuthenticated();

  useEffect(() => {
    if (!authenticated) {
      router.replace(LOGIN_PATH);
    }
  }, [authenticated, router]);

  if (!authenticated) return null;

  return <>{children}</>;
}
