"use server";

import { revalidatePath } from "next/cache";
import { cookies } from "next/headers";
import { DEFAULT_LOCALE, LOCALE_COOKIE, LOCALE_COOKIE_MAX_AGE, isLocale } from "@/lib/locale";

/** Writes the locale cookie and refreshes server components (docs/14 §9). */
export async function setLocale(value: string): Promise<void> {
  const locale = isLocale(value) ? value : DEFAULT_LOCALE;
  const store = await cookies();
  store.set(LOCALE_COOKIE, locale, {
    path: "/",
    sameSite: "lax",
    maxAge: LOCALE_COOKIE_MAX_AGE,
    httpOnly: false, // the toggle may read it client-side
  });
  revalidatePath("/", "layout");
}
