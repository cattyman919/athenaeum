"use client";

import * as React from "react";
import { toast } from "sonner";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ApiError, api } from "@/lib/api-client";
import { ancestorIds, locateShelf, statusLabel } from "@/lib/admin";
import type { CopyStatus, ShelfInspectorData } from "@/lib/admin-types";
import type { AdminTree } from "@/lib/intake-types";
import { cn } from "@/lib/utils";
import { BuildingTree } from "./building-tree";
import { FloorMiniMap } from "./floor-minimap";
import { ShelfInspector } from "./shelf-inspector";

interface Props {
  initialTree: AdminTree;
  initialShelf: ShelfInspectorData | null;
}

/**
 * Screen 6b stacks manager: tree browser + mini-map + shelf inspector. The selected shelf
 * is URL state (`?shelf=<id>`) so a reload or shared link reopens the same manifest.
 */
export function LayoutManager({ initialTree, initialShelf }: Props) {
  const [tree, setTree] = React.useState(initialTree);
  const [selectedId, setSelectedId] = React.useState<string | null>(initialShelf?.id ?? null);
  const [shelf, setShelf] = React.useState<ShelfInspectorData | null>(initialShelf);
  const [loading, setLoading] = React.useState(false);
  const [expanded, setExpanded] = React.useState<Set<string>>(() => {
    const ids = initialShelf ? ancestorIds(initialTree, initialShelf.id) : [];
    return new Set(ids.length ? ids : initialTree.floors.slice(0, 1).map((f) => f.id));
  });
  const [mapFloorId, setMapFloorId] = React.useState<string | null>(() => {
    const loc = initialShelf ? locateShelf(initialTree, initialShelf.id) : null;
    return loc?.floor.id ?? initialTree.floors[0]?.id ?? null;
  });
  const requestRef = React.useRef(0);

  const loadShelf = React.useCallback(async (id: string) => {
    const req = ++requestRef.current;
    setLoading(true);
    try {
      const data = await api.adminShelf(id);
      if (req === requestRef.current) setShelf(data);
    } catch (err) {
      if (req === requestRef.current) {
        setShelf(null);
        toast.error(err instanceof ApiError ? err.message : "Could not load the shelf.");
      }
    } finally {
      if (req === requestRef.current) setLoading(false);
    }
  }, []);

  const refreshTree = React.useCallback(async () => {
    try {
      setTree(await api.adminTree());
    } catch {
      // Occupancy counts stay stale until the next successful refresh.
    }
  }, []);

  const selectShelf = React.useCallback(
    (id: string) => {
      setSelectedId(id);
      setExpanded((cur) => new Set([...cur, ...ancestorIds(tree, id)]));
      const loc = locateShelf(tree, id);
      if (loc) setMapFloorId(loc.floor.id);
      const url = new URL(window.location.href);
      url.searchParams.set("shelf", id);
      window.history.replaceState(null, "", url);
      void loadShelf(id);
    },
    [tree, loadShelf],
  );

  const toggle = React.useCallback((id: string) => {
    setExpanded((cur) => {
      const next = new Set(cur);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  async function assign(barcode: string): Promise<string | null> {
    if (!selectedId) return "Select a shelf first.";
    try {
      await api.patchAdminCopy(barcode, { shelf_id: selectedId });
      toast.success(`Assigned ${barcode} to ${shelf?.code ?? "shelf"}.`);
      await Promise.all([loadShelf(selectedId), refreshTree()]);
      return null;
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) return `No copy with barcode ${barcode}.`;
      return err instanceof ApiError ? err.message : "Could not assign the copy.";
    }
  }

  async function changeStatus(copyId: string, status: CopyStatus) {
    try {
      const updated = await api.patchAdminCopy(copyId, { status });
      toast.success(`${updated.barcode} → ${statusLabel(status)}.`);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Could not change the status.");
    }
    if (selectedId) await loadShelf(selectedId);
  }

  const mapFloor = tree.floors.find((f) => f.id === mapFloorId) ?? tree.floors[0];
  const selectedLoc = selectedId ? locateShelf(tree, selectedId) : null;

  return (
    <div className="grid gap-6 lg:grid-cols-[minmax(0,20rem)_minmax(0,1fr)_minmax(0,24rem)]">
      <Card className="lg:row-span-2">
        <CardContent className="max-h-[70vh] overflow-y-auto p-3">
          <BuildingTree
            tree={tree}
            expanded={expanded}
            onToggle={toggle}
            selectedShelfId={selectedId}
            onSelectShelf={selectShelf}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex-row items-center justify-between space-y-0 pb-3">
          <CardTitle className="text-base">Floor mini-map</CardTitle>
          <div className="flex gap-1" role="group" aria-label="Floor">
            {tree.floors.map((f) => (
              <button
                key={f.id}
                type="button"
                onClick={() => setMapFloorId(f.id)}
                aria-pressed={f.id === mapFloor?.id}
                className={cn(
                  "rounded-md px-2 py-1 font-mono text-xs text-muted-foreground hover:bg-surface-low",
                  f.id === mapFloor?.id && "bg-secondary text-foreground",
                )}
              >
                {f.floor_number}F
              </button>
            ))}
          </div>
        </CardHeader>
        <CardContent>
          {mapFloor ? (
            <FloorMiniMap
              floor={mapFloor}
              selectedShelfId={selectedId}
              selectedAisleId={selectedLoc?.aisle.id ?? null}
              onSelectShelf={selectShelf}
            />
          ) : (
            <p className="text-sm text-muted-foreground">No floors configured.</p>
          )}
        </CardContent>
      </Card>

      <div className="lg:row-span-2">
        <ShelfInspector
          shelf={shelf}
          loading={loading}
          onAssign={assign}
          onStatus={changeStatus}
          onSelectShelf={selectShelf}
        />
      </div>
    </div>
  );
}
