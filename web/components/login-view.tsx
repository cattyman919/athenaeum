"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { useTranslations } from "next-intl";
import { ArrowRight, Eye, EyeOff, IdCard, Library, Lock, LogIn, MessageCircle } from "lucide-react";
import { api, ApiError } from "@/lib/api-client";
import { LanguageToggle } from "@/components/language-toggle";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";

type Role = "patron" | "faculty" | "staff";
type Mode = "login" | "register";

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function LoginView({ next, notice }: { next: string; notice: "sso" | "expired" | null }) {
  const t = useTranslations("login");
  const tc = useTranslations("common");
  const router = useRouter();
  const [mode, setMode] = React.useState<Mode>("login");
  const [role, setRole] = React.useState<Role>("patron");
  const [name, setName] = React.useState("");
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [showPassword, setShowPassword] = React.useState(false);
  const [submitting, setSubmitting] = React.useState(false);
  const [errorCode, setErrorCode] = React.useState<string | null>(notice);

  const emailValid = EMAIL_RE.test(email.trim());

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    setErrorCode(null);
    try {
      if (mode === "login") await api.login(email.trim(), password);
      else await api.register(name.trim(), email.trim(), password);
      router.push(next);
      router.refresh();
    } catch (err) {
      setErrorCode(err instanceof ApiError ? err.code : "UNEXPECTED");
      setSubmitting(false);
    }
  }

  const errorText = errorCode
    ? t.has(`errors.${errorCode}`)
      ? t(`errors.${errorCode}`)
      : t("errors.UNEXPECTED")
    : null;

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-surface p-6">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 opacity-40 [background:radial-gradient(ellipse_80%_60%_at_50%_-10%,rgba(0,74,198,0.12),transparent)]"
      />
      <div
        aria-hidden
        className="pointer-events-none absolute -left-32 -top-32 h-96 w-96 rounded-full bg-primary/10 blur-3xl"
      />
      <div
        aria-hidden
        className="pointer-events-none absolute -bottom-32 -right-32 h-96 w-96 rounded-full bg-accent/10 blur-3xl"
      />

      <div className="absolute right-6 top-6 z-20">
        <LanguageToggle />
      </div>

      <div className="relative z-10 mx-auto flex w-full max-w-lg flex-col gap-6">
        <div className="flex flex-col items-center gap-1 text-center">
          <div className="flex items-center gap-3">
            <span className="flex h-10 w-10 items-center justify-center rounded-xl bg-primary/10 text-primary shadow-sm">
              <Library className="h-6 w-6" aria-hidden />
            </span>
            <span className="text-2xl font-semibold tracking-tight">{tc("appName")}</span>
          </div>
          <span className="mono-label text-outline">{tc("tagline")}</span>
        </div>

        <div className="flex flex-col rounded-xl bg-surface-lowest p-6 shadow-xl sm:p-8">
          <div className="mb-6 flex flex-col items-center text-center">
            <div className="mono-label mb-1 flex items-center gap-1 text-outline">
              <span>{t("badge")}</span>
              <span className="text-outline-variant">•</span>
              <span>{t("est")}</span>
            </div>
            <h1 className="text-3xl font-bold tracking-tight">
              {mode === "login" ? t("title") : t("registerTitle")}
            </h1>
            <p className="mt-1.5 max-w-sm text-sm leading-relaxed text-on-surface-variant">
              {mode === "login" ? t("subtitle") : t("registerSubtitle")}
            </p>
          </div>

          <Tabs value={role} onValueChange={(v) => setRole(v as Role)} className="mb-6">
            <TabsList aria-label={t("roles.label")}>
              <TabsTrigger value="patron">{t("roles.patron")}</TabsTrigger>
              <TabsTrigger value="faculty">{t("roles.faculty")}</TabsTrigger>
              <TabsTrigger value="staff">{t("roles.staff")}</TabsTrigger>
            </TabsList>
          </Tabs>

          {errorText && (
            <div
              role="alert"
              data-testid="login-error"
              className="mb-4 rounded-lg border border-destructive/30 bg-status-na-bg px-3 py-2 text-sm text-status-na-fg"
            >
              {errorText}
            </div>
          )}

          <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
            {mode === "register" && (
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="name">{t("fields.name")}</Label>
                <Input
                  id="name"
                  name="name"
                  autoComplete="name"
                  required
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder={t("fields.namePlaceholder")}
                />
              </div>
            )}

            <div className="flex flex-col gap-1.5">
              <Label htmlFor="email">{t("fields.email")}</Label>
              <div className="relative flex items-center">
                <IdCard
                  className="pointer-events-none absolute left-3 h-[18px] w-[18px] text-outline"
                  aria-hidden
                />
                <Input
                  id="email"
                  name="email"
                  type="email"
                  autoComplete="email"
                  required
                  className="pl-10"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder={t(`fields.emailPlaceholder.${role}`)}
                />
                {emailValid && (
                  <span className="mono-label absolute right-3 flex items-center gap-1 rounded bg-surface-container px-1.5 py-0.5 text-[10px] text-status-available-fg">
                    <span className="h-1.5 w-1.5 rounded-full bg-status-available-dot" />
                    {t("fields.emailValid")}
                  </span>
                )}
              </div>
            </div>

            <div className="flex flex-col gap-1.5">
              <Label htmlFor="password">{t("fields.password")}</Label>
              <div className="relative flex items-center">
                <Lock
                  className="pointer-events-none absolute left-3 h-[18px] w-[18px] text-outline"
                  aria-hidden
                />
                <Input
                  id="password"
                  name="password"
                  type={showPassword ? "text" : "password"}
                  autoComplete={mode === "login" ? "current-password" : "new-password"}
                  required
                  className="px-10 font-mono"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder={t("fields.passwordPlaceholder")}
                />
                <button
                  type="button"
                  aria-label={showPassword ? t("fields.hidePassword") : t("fields.showPassword")}
                  onClick={() => setShowPassword((s) => !s)}
                  className="absolute right-3 rounded p-1 text-outline transition-colors hover:text-on-surface focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  {showPassword ? (
                    <EyeOff className="h-[18px] w-[18px]" />
                  ) : (
                    <Eye className="h-[18px] w-[18px]" />
                  )}
                </button>
              </div>
              {mode === "register" ? (
                <p className="text-xs text-muted-foreground">{t("fields.passwordHint")}</p>
              ) : (
                <p className="text-xs text-muted-foreground">{t("forgot")}</p>
              )}
            </div>

            <Button
              type="submit"
              size="lg"
              disabled={submitting}
              data-testid="login-submit"
              className="mt-2 w-full shadow-md"
            >
              <span>
                {submitting
                  ? t("submitting")
                  : mode === "login"
                    ? t("submit")
                    : t("submitRegister")}
              </span>
              {!submitting && <ArrowRight className="h-[18px] w-[18px]" aria-hidden />}
            </Button>
          </form>

          <div className="relative my-6 flex items-center justify-center">
            <div className="h-px w-full bg-outline-variant/60" />
            <span className="mono-label absolute bg-surface-lowest px-3 text-outline">
              {t("divider")}
            </span>
          </div>

          <div className="flex flex-col gap-2">
            <a
              href={`/api/auth/google?next=${encodeURIComponent(next)}`}
              data-testid="google-sso"
              className="group flex h-11 w-full items-center justify-between rounded-lg bg-surface-low px-4 text-sm font-medium shadow-sm transition-all hover:bg-surface-container focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <span className="flex items-center gap-2">
                <GoogleMark />
                <span>{t("google")}</span>
              </span>
              <LogIn className="h-4 w-4 text-outline group-hover:text-on-surface" aria-hidden />
            </a>

            <div className="group/wechat relative w-full">
              <button
                type="button"
                disabled
                aria-disabled="true"
                data-testid="wechat-sso"
                className="flex h-11 w-full cursor-not-allowed items-center justify-between rounded-lg bg-surface-container/50 px-4 text-sm font-medium text-outline opacity-75"
              >
                <span className="flex items-center gap-2">
                  <MessageCircle className="h-[18px] w-[18px]" aria-hidden />
                  <span>{t("wechat")}</span>
                </span>
                <span className="mono-label flex items-center gap-1 rounded bg-surface-highest px-2 py-0.5">
                  <span className="h-1.5 w-1.5 rounded-full bg-outline-variant" />
                  {t("unavailable")}
                </span>
              </button>
              <div
                role="tooltip"
                className="pointer-events-none absolute -top-10 left-1/2 z-50 hidden -translate-x-1/2 whitespace-nowrap rounded-lg px-3 py-1 text-xs text-white shadow-xl group-hover/wechat:flex"
                style={{ backgroundColor: "#2f3038" }}
              >
                {t("wechatTooltip")}
              </div>
            </div>
          </div>

          <div className="mt-6 flex flex-col items-center gap-2 border-t border-outline-variant/40 pt-4 text-center text-sm text-on-surface-variant">
            <p>
              {mode === "login" ? t("noAccount") : t("haveAccount")}{" "}
              <button
                type="button"
                data-testid="toggle-mode"
                onClick={() => {
                  setMode(mode === "login" ? "register" : "login");
                  setErrorCode(null);
                }}
                className="font-medium text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                {mode === "login" ? t("registerLink") : t("signInLink")}
              </button>
            </p>
          </div>
        </div>

        <div className="flex items-center justify-between gap-4 rounded-xl bg-surface-lowest/80 p-4 shadow-md backdrop-blur-md">
          <div className="flex min-w-0 flex-col">
            <div className="flex items-center gap-1.5">
              <span className="h-2 w-2 rounded-full bg-status-available-dot" />
              <span className="mono-label text-status-available-fg">{t("spotlight.badge")}</span>
            </div>
            <span className="mt-0.5 truncate text-base font-semibold">{t("spotlight.title")}</span>
            <span className="mono-label mt-0.5 truncate text-outline">{t("spotlight.meta")}</span>
          </div>
          <div className="hidden shrink-0 flex-col items-end sm:flex">
            <span className="mono-label rounded bg-surface-container px-2 py-1 font-semibold text-on-surface">
              {t("spotlight.copies")}
            </span>
            <span className="mt-1 text-[11px] text-outline">{t("spotlight.pickup")}</span>
          </div>
        </div>

        <p className="mono-label text-center text-outline">{t("gateway")}</p>
      </div>
    </div>
  );
}

function GoogleMark() {
  return (
    <svg className="h-4 w-4 shrink-0" viewBox="0 0 24 24" aria-hidden>
      <path
        d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z"
        fill="#4285F4"
      />
      <path
        d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"
        fill="#34A853"
      />
      <path
        d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.06H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.94l2.85-2.22.81-.63z"
        fill="#FBBC05"
      />
      <path
        d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.06l3.66 2.84c.87-2.6 3.3-4.52 6.16-4.52z"
        fill="#EA4335"
      />
    </svg>
  );
}
