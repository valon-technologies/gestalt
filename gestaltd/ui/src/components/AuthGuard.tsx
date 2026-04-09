"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { isAuthenticated } from "@/lib/auth";
import { LOGIN_PATH } from "@/lib/constants";

export default function AuthGuard({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const [checked, setChecked] = useState(false);
  const [authenticated, setAuthenticated] = useState(false);

  useEffect(() => {
    const authed = isAuthenticated();
    setAuthenticated(authed);
    setChecked(true);
    if (!authed) {
      router.replace(LOGIN_PATH);
    }
  }, [router]);

  if (!checked || !authenticated) return null;

  return <>{children}</>;
}
