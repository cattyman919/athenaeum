// Admin payload types (docs/03 §8.1, §8.3–8.5). The tree shape lives in intake-types.ts.

import type { PageMeta, SectionRef } from "./types";

export type CopyStatus = "available" | "checked_out" | "reserved_waitlist" | "missing_lost";

export const COPY_STATUSES: readonly CopyStatus[] = [
  "available",
  "checked_out",
  "reserved_waitlist",
  "missing_lost",
];

/** GET /api/admin/data. */
export interface AdminKpis {
  titles_in_catalog: number;
  copies: { total: number; available: number; checked_out: number; missing_lost: number };
  shelf_capacity: { occupied: number; total: number };
  active_patrons: number;
  chat_week: { conversations: number; messages: number };
  computed_at: string;
}

/** One GET /api/admin/copy-events item (newest first). */
export interface CopyEvent {
  id: number;
  barcode: string;
  book_title: string;
  from_status: string | null;
  to_status: string;
  source: string;
  actor: { id: string; name: string } | null;
  created_at: string;
}

export interface Occupancy {
  occupied: number;
  total: number;
}

export interface ShelfCopy {
  id: string;
  barcode: string;
  status: string;
  book_id: string;
  title: string;
}

/** GET /api/admin/shelves/{id}. */
export interface ShelfInspectorData {
  id: string;
  code: string;
  path: string;
  aisle: string;
  floor: number;
  section: SectionRef;
  row_level: number;
  height_tier: string;
  occupancy: Occupancy;
  copies: ShelfCopy[];
  adjacent: Array<{ id: string; code: string; path: string; occupancy: Occupancy }>;
}

/** PATCH /api/admin/copies/{id|barcode} body. */
export interface CopyPatch {
  status?: CopyStatus;
  condition?: string;
  shelf_id?: string | null;
}

/** POST /api/admin/copies body. */
export interface CopyCreate {
  book_id: string;
  barcode?: string;
  shelf_path?: string;
  status?: CopyStatus;
  condition?: string;
}

/** Admin copy row returned by POST/PATCH copies. */
export interface AdminCopy {
  id: string;
  book_id: string;
  barcode: string;
  status: CopyStatus;
  condition: string;
  shelf_id: string | null;
  last_state_change: string;
}

export type UserRole = "user" | "admin";

/** GET /api/admin/users item. */
export interface AdminUser {
  id: string;
  email: string;
  name: string;
  role: UserRole;
  created_at: string;
  library_count: number;
  waitlist_count: number;
  last_login_at: string | null;
}

export interface AdminUserPage {
  items: AdminUser[];
  meta: PageMeta;
}

/** GET /api/admin/books item (subset relied on by the UI). */
export interface AdminBookItem {
  id: string;
  title: string;
  authors: string[];
  isbn13: string | null;
  section: SectionRef | null;
  availability: { total_copies: number; available_copies: number; state: string };
  updated_at: string;
  active_loans: number;
  removable: boolean;
}

/** GET /api/admin/index-status — the auto-index worker backlog (docs/03 §8.1, docs/06 §2). */
export interface IndexStatus {
  pending_count: number;
  pending_book_ids: string[];
}

export interface AdminBookPage {
  items: AdminBookItem[];
  meta: PageMeta;
}
