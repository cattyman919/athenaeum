import Link from "next/link";
import { ArrowRight, Map } from "lucide-react";
import { AdminBooksTable } from "@/components/admin/admin-books-table";
import { AdminForbidden } from "@/components/admin/admin-forbidden";
import {
  IndexStatusProvider,
  IndexingChip,
  RecentlyAddedBooks,
} from "@/components/admin/index-status";
import { AdminNav } from "@/components/admin/admin-nav";
import { RealTimeAuditStream } from "@/components/admin/audit-stream";
import { KpiGrid } from "@/components/admin/kpi-grid";
import { UsersTable } from "@/components/admin/users-table";
import { StaffIntakeButton } from "@/components/barcode-scanner";
import { AddBookDialog } from "@/components/isbn-scanner";
import { Alert } from "@/components/ui/alert";
import { buttonVariants } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { AUDIT_MAX_ROWS } from "@/lib/admin";
import { adminGet } from "@/lib/admin-server";
import type { AdminKpis, AdminUserPage, CopyEvent } from "@/lib/admin-types";
import { getCurrentUser } from "@/lib/api-server";

// Screen 6a — Stitch 3177da52… Admin (docs/07 §7, docs/12 screen 6). Admin copy stays English
// (docs/14 §1); only the non-admin FORBIDDEN state is localized. KPI grid (GET /api/admin/data),
// RealTimeAuditStream (GET /api/admin/copy-events, 3 s poll), accounts table
// (GET/PATCH /api/admin/users), the Add Book dialog with the ISBN scanner tab (docs/11 §5) and
// the shared scanner dialog opened on Staff Intake (docs/07 §Scanner dialog), and the
// auto-index backlog chip/badges (GET /api/admin/index-status, 4 s poll while pending).
export default async function AdminPage() {
  const user = await getCurrentUser();
  if (user?.role !== "admin") return <AdminForbidden />;

  const [kpis, events, users] = await Promise.all([
    adminGet<AdminKpis>("/api/admin/data"),
    adminGet<{ items: CopyEvent[] }>(`/api/admin/copy-events?limit=${AUDIT_MAX_ROWS}`),
    adminGet<AdminUserPage>("/api/admin/users?per_page=50"),
  ]);

  return (
    <IndexStatusProvider>
      <div className="space-y-6" data-testid="admin-page">
        <header className="flex flex-wrap items-end justify-between gap-4">
          <div className="space-y-3">
            <div>
              <p className="text-xs font-medium uppercase tracking-wider text-on-surface-variant">
                Staff Admin
              </p>
              <h1 className="text-2xl font-semibold">Collection management</h1>
            </div>
            <AdminNav />
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <IndexingChip />
            <StaffIntakeButton />
            <AddBookDialog />
          </div>
        </header>

        {kpis ? (
          <KpiGrid data={kpis} />
        ) : (
          <Alert variant="destructive" data-testid="kpi-error">
            Could not load the collection metrics. Refresh to retry.
          </Alert>
        )}

        <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,22rem)]">
          <div className="space-y-3">
            {!events && (
              <Alert variant="warning" data-testid="audit-error">
                Could not load recent copy events. The stream keeps polling and will catch up.
              </Alert>
            )}
            <RealTimeAuditStream initial={events?.items ?? []} />
          </div>
          <div className="h-fit space-y-6">
            <RecentlyAddedBooks />
            <Card>
              <CardContent className="space-y-3 p-5">
                <div className="flex items-center gap-2 font-semibold">
                  <Map className="h-4 w-4 text-primary" aria-hidden />
                  Stacks manager
                </div>
                <p className="text-sm text-muted-foreground">
                  Browse the building tree, inspect shelf manifests, re-shelve copies by barcode and
                  update copy status.
                </p>
                <Link
                  href="/admin/layout"
                  className={buttonVariants({ variant: "outline", size: "sm" })}
                  data-testid="to-layout"
                >
                  Open layout manager
                  <ArrowRight className="h-4 w-4" aria-hidden />
                </Link>
              </CardContent>
            </Card>
          </div>
        </div>

        <AdminBooksTable />

        {users ? (
          <UsersTable initial={users.items} currentUserId={user.id} />
        ) : (
          <Alert variant="destructive" data-testid="users-error">
            Could not load the accounts list. Refresh to retry.
          </Alert>
        )}
      </div>
    </IndexStatusProvider>
  );
}
