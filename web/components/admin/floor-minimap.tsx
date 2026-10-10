"use client";

import * as React from "react";
import { MINIMAP, minimapLayout, occupancyLevel } from "@/lib/admin";
import type { AdminTree } from "@/lib/intake-types";

type Floor = AdminTree["floors"][number];

const LEVEL_FILL: Record<string, string> = {
  empty: "#e3e1ec",
  low: "#86efac",
  mid: "#60a5fa",
  high: "#fb923c",
  full: "#f87171",
};

interface Props {
  floor: Floor;
  selectedShelfId: string | null;
  selectedAisleId: string | null;
  onSelectShelf: (id: string) => void;
}

/** Floor mini-map: a simple SVG grid highlighting the selected aisle (docs/07 screen 6b). */
export function FloorMiniMap({ floor, selectedShelfId, selectedAisleId, onSelectShelf }: Props) {
  const m = React.useMemo(() => minimapLayout(floor), [floor]);
  return (
    <figure data-testid="floor-minimap" data-floor={floor.floor_number} className="space-y-2">
      <svg
        viewBox={`0 0 ${m.width} ${m.height}`}
        className="h-auto w-full"
        role="img"
        aria-label={`Floor ${floor.floor_number} map`}
      >
        {m.sections.map((s) => (
          <g key={s.id}>
            <rect
              x={s.x}
              y={s.y}
              width={s.w}
              height={s.h}
              rx={6}
              fill={s.color}
              fillOpacity={0.08}
              stroke={s.color}
              strokeOpacity={0.5}
            />
            <text
              x={s.x + MINIMAP.pad}
              y={s.y + MINIMAP.pad + 8}
              fontSize={9}
              fontFamily="monospace"
              fill={s.color}
            >
              {s.code}
            </text>
          </g>
        ))}
        {m.aisles.map((a) => {
          const active = a.id === selectedAisleId;
          return (
            <rect
              key={a.id}
              x={a.x}
              y={a.y - 2}
              width={a.w}
              height={a.h + 4}
              rx={3}
              fill={active ? "#2563eb" : "transparent"}
              fillOpacity={active ? 0.15 : 0}
              stroke={active ? "#2563eb" : "#c3c6d7"}
              strokeWidth={active ? 1.5 : 0.75}
              data-testid="minimap-aisle"
              data-active={active ? "true" : undefined}
            >
              <title>Aisle {a.code}</title>
            </rect>
          );
        })}
        {m.shelves.map((s) => {
          const selected = s.id === selectedShelfId;
          return (
            <rect
              key={s.id}
              x={s.x}
              y={s.y}
              width={s.w}
              height={s.h}
              rx={2}
              fill={LEVEL_FILL[occupancyLevel(s.occupancy)]}
              stroke={selected ? "#1a1b22" : "none"}
              strokeWidth={selected ? 1.5 : 0}
              className="cursor-pointer"
              onClick={() => onSelectShelf(s.id)}
              data-testid="minimap-shelf"
              data-selected={selected ? "true" : undefined}
            >
              <title>
                {s.code} · {s.occupancy.occupied}/{s.occupancy.total}
              </title>
            </rect>
          );
        })}
      </svg>
      <figcaption className="flex flex-wrap items-center gap-3 text-[11px] text-muted-foreground">
        <span className="font-medium text-foreground">
          Floor {floor.floor_number} · {floor.name}
        </span>
        {(["empty", "low", "mid", "high", "full"] as const).map((l) => (
          <span key={l} className="flex items-center gap-1">
            <span
              className="h-2 w-2 rounded-sm"
              style={{ backgroundColor: LEVEL_FILL[l] }}
              aria-hidden
            />
            {l}
          </span>
        ))}
      </figcaption>
    </figure>
  );
}
