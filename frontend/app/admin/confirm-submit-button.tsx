"use client";

import type { MouseEvent, ReactNode } from "react";

export function ConfirmSubmitButton({
  children,
  message,
  className,
  disabled = false,
}: {
  children: ReactNode;
  message: string;
  className: string;
  disabled?: boolean;
}) {
  function confirmSubmit(event: MouseEvent<HTMLButtonElement>) {
    if (!window.confirm(message)) event.preventDefault();
  }

  return (
    <button className={className} disabled={disabled} onClick={confirmSubmit} type="submit">
      {children}
    </button>
  );
}
