import { AdminForbidden } from "@/components/admin/admin-forbidden";
import { AdminNav } from "@/components/admin/admin-nav";
import { LayoutManager } from "@/components/admin/layout-manager";
import { Alert } from "@/components/ui/alert";
import { adminGet } from "@/lib/admin-server";
import type { ShelfInspectorData } from "@/lib/admin-types";
import { getCurrentUser } from "@/lib/api-server";
import type { AdminTree } from "@/lib/intake-types";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

// Screen 6b — Stitch 3177da52… stacks manager (docs/07 §7, docs/12 screen 6, docs/05 §2).
// BuildingTreeBrowser (GET /api/admin/tree), shelf manifest inspector
// (GET /api/admin/shelves/{id}, quick-assign via PATCH /api/admin/copies/{barcode}) and a
// floor mini-map. `?shelf=<id>` is the selected shelf (URL-as-state). English only (docs/14 §1).
export default async function AdminLayoutPage({
  searchParams,
}: {
  searchParams: Promise<{ shelf?: string }>;
}) {
  const user = await getCurrentUser();
  if (user?.role !== "admin") return <AdminForbidden />;

  const { shelf: shelfParam } = await searchParams;
  const shelfId = shelfParam && UUID.test(shelfParam) ? shelfParam : null;
  const [tree, shelf] = await Promise.all([
    adminGet<AdminTree>("/api/admin/tree"),
    shelfId ? adminGet<ShelfInspectorData>(`/api/admin/shelves/${shelfId}`) : Promise.resolve(null),
  ]);

  return (
    <div className="space-y-6" data-testid="admin-layout-page">
      <header className="space-y-3">
        <div>
          <p className="text-xs font-medium uppercase tracking-wider text-on-surface-variant">
            Staff Admin
          </p>
          <h1 className="text-2xl font-semibold">Stacks manager</h1>
        </div>
        <AdminNav />
      </header>
      {tree ? (
        <LayoutManager initialTree={tree} initialShelf={shelf} />
      ) : (
        <Alert variant="destructive" data-testid="tree-error">
          Could not load the building layout. Refresh to retry.
        </Alert>
      )}
    </div>
  );
}
