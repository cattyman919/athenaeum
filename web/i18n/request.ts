import { getRequestConfig } from "next-intl/server";
import { cookies, headers } from "next/headers";
import { LOCALE_COOKIE, resolveLocale } from "@/lib/locale";

// Locale resolution without URL routing (docs/14 §3–4): cookie → Accept-Language → en.
export default getRequestConfig(async () => {
  const [cookieStore, headerStore] = await Promise.all([cookies(), headers()]);
  const locale = resolveLocale({
    cookie: cookieStore.get(LOCALE_COOKIE)?.value,
    acceptLanguage: headerStore.get("accept-language"),
  });
  return {
    locale,
    messages: (await import(`../messages/${locale}.json`)).default,
    timeZone: "America/New_York", // library building time zone (docs/14 §7)
  };
});
