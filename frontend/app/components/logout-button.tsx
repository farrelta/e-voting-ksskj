"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

export function LogoutButton() {
  const router = useRouter();
  const [error, setError] = useState("");

  async function logout() {
    setError("");
    try {
      const response = await fetch("/api/logout", { method: "POST" });
      if (!response.ok) {
        const payload = (await response.json()) as { error?: string };
        throw new Error(payload.error || "Logout failed.");
      }
      router.replace("/login");
      router.refresh();
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Logout failed.");
    }
  }

  return (
    <div className="logout-control">
      <button className="button button-secondary button-small" onClick={logout}>
        Sign out
      </button>
      {error && <span className="inline-error">{error}</span>}
    </div>
  );
}
