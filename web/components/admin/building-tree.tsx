"use client";

import * as React from "react";
import { Building2, ChevronRight, Layers } from "lucide-react";
import { floorShelves, occupancyLevel, rollup, sectionShelves } from "@/lib/admin";
import type { Occupancy } from "@/lib/admin-types";
import type { AdminTree } from "@/lib/intake-types";
import { cn } from "@/lib/utils";

const LEVEL_DOT: Record<string, string> = {
  empty: "bg-surface-highest",
  low: "bg-status-available-dot",
  mid: "bg-primary",
  high: "bg-status-out-dot",
  full: "bg-status-na-dot",
};

const ROW =
  "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-surface-low focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring";

function Count({ o }: { o: Occupancy }) {
  return (
    <span className="ml-auto shrink-0 font-mono text-[11px] tabular-nums text-muted-foreground">
      {o.occupied}/{o.total}
    </span>
  );
}

interface BranchProps {
  id: string;
  label: string;
  level: number;
  icon: React.ReactNode;
  occupancy: Occupancy;
  testId: string;
  code: string;
  open: boolean;
  onToggle: (id: string) => void;
  children: React.ReactNode;
}

function Branch({
  id,
  label,
  level,
  icon,
  occupancy,
  testId,
  code,
  open,
  onToggle,
  children,
}: BranchProps) {
  return (
    <li
      role="treeitem"
      aria-expanded={open}
      aria-level={level}
      aria-selected={false}
      data-testid={testId}
      data-code={code}
    >
      <button type="button" className={ROW} onClick={() => onToggle(id)}>
        <ChevronRight
          className={cn(
            "h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform",
            open && "rotate-90",
          )}
          aria-hidden
        />
        {icon}
        <span className="truncate">{label}</span>
        <Count o={occupancy} />
      </button>
      {open && (
        <ul role="group" className="ml-3 border-l border-border pl-2">
          {children}
        </ul>
      )}
    </li>
  );
}

interface Props {
  tree: AdminTree;
  expanded: Set<string>;
  onToggle: (id: string) => void;
  selectedShelfId: string | null;
  onSelectShelf: (id: string) => void;
}

/** BuildingTreeBrowser: Building → Floors → Sections → Aisles → Shelves (docs/05 §2). */
export function BuildingTree({ tree, expanded, onToggle, selectedShelfId, onSelectShelf }: Props) {
  return (
    <div data-testid="building-tree">
      <div className="mb-2 flex items-center gap-2 px-2 text-sm font-semibold">
        <Building2 className="h-4 w-4 text-primary" aria-hidden />
        <span>{tree.name}</span>
        <span className="mono-label text-muted-foreground">{tree.code}</span>
      </div>
      <ul role="tree" aria-label="Building layout">
        {tree.floors.map((floor) => (
          <Branch
            key={floor.id}
            id={floor.id}
            label={`Floor ${floor.floor_number} · ${floor.name}`}
            level={1}
            icon={<Layers className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />}
            occupancy={rollup(floorShelves(floor))}
            testId="tree-floor"
            code={String(floor.floor_number)}
            open={expanded.has(floor.id)}
            onToggle={onToggle}
          >
            {floor.sections.map((section) => (
              <Branch
                key={section.id}
                id={section.id}
                label={`${section.code} · ${section.name}`}
                level={2}
                icon={
                  <span
                    className="h-2.5 w-2.5 shrink-0 rounded-full"
                    style={{ backgroundColor: section.color }}
                    aria-hidden
                  />
                }
                occupancy={rollup(sectionShelves(section))}
                testId="tree-section"
                code={section.code}
                open={expanded.has(section.id)}
                onToggle={onToggle}
              >
                {section.aisles.map((aisle) => (
                  <Branch
                    key={aisle.id}
                    id={aisle.id}
                    label={`Aisle ${aisle.code}`}
                    level={3}
                    icon={null}
                    occupancy={rollup(aisle.shelves)}
                    testId="tree-aisle"
                    code={aisle.code}
                    open={expanded.has(aisle.id)}
                    onToggle={onToggle}
                  >
                    {aisle.shelves.map((shelf) => {
                      const selected = shelf.id === selectedShelfId;
                      const o = { occupied: shelf.occupied, total: shelf.total };
                      return (
                        <li key={shelf.id} role="treeitem" aria-level={4} aria-selected={selected}>
                          <button
                            type="button"
                            className={cn(
                              ROW,
                              selected && "bg-primary/10 font-medium text-primary",
                            )}
                            onClick={() => onSelectShelf(shelf.id)}
                            data-testid="tree-shelf"
                            data-path={shelf.path}
                            aria-current={selected ? "true" : undefined}
                          >
                            <span
                              className={cn(
                                "h-2 w-2 shrink-0 rounded-full",
                                LEVEL_DOT[occupancyLevel(o)],
                              )}
                              aria-hidden
                            />
                            <span className="font-mono text-xs">{shelf.code}</span>
                            <span className="truncate text-xs text-muted-foreground">
                              Row {shelf.row_level} · {shelf.height_tier}
                            </span>
                            <Count o={o} />
                          </button>
                        </li>
                      );
                    })}
                  </Branch>
                ))}
              </Branch>
            ))}
          </Branch>
        ))}
      </ul>
    </div>
  );
}
