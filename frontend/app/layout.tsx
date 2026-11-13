import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "E-Voting",
  description: "Secure election registration and ballot management",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
