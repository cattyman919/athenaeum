import type { Metadata } from "next";
import { Inter, JetBrains_Mono, Noto_Sans_SC } from "next/font/google";
import { NextIntlClientProvider } from "next-intl";
import { getLocale, getMessages } from "next-intl/server";
import { AuthProvider } from "@/providers/auth-provider";
import { Toaster } from "@/components/ui/sonner";
import { getCurrentUser } from "@/lib/api-server";
import "./globals.css";

const inter = Inter({ subsets: ["latin"], variable: "--font-inter", display: "swap" });
const mono = JetBrains_Mono({ subsets: ["latin"], variable: "--font-jetbrains", display: "swap" });
// Inter has no CJK glyphs; Noto Sans SC is the zh-CN fallback (docs/14 §8).
const notoSc = Noto_Sans_SC({
  weight: ["400", "500", "700"],
  variable: "--font-noto-sc",
  display: "swap",
  preload: false,
});

export const metadata: Metadata = {
  title: "Athenaeum Library",
  description: "Academic catalog & wayfinding system",
};

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  const [locale, messages, user] = await Promise.all([
    getLocale(),
    getMessages(),
    getCurrentUser(),
  ]);
  return (
    <html lang={locale} className={`${inter.variable} ${mono.variable} ${notoSc.variable}`}>
      <body className="min-h-screen font-sans">
        <NextIntlClientProvider locale={locale} messages={messages}>
          <AuthProvider initialUser={user}>{children}</AuthProvider>
          <Toaster />
        </NextIntlClientProvider>
      </body>
    </html>
  );
}
