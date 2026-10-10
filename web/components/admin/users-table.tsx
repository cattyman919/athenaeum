"use client";

import * as React from "react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ApiError, api } from "@/lib/api-client";
import { canChangeRole } from "@/lib/admin";
import type { AdminUser, UserRole } from "@/lib/admin-types";

const selectClass =
  "h-8 rounded-md border border-border bg-card px-2 text-xs shadow-sm focus:outline-none focus:ring-2 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-50";

const dateFmt = (iso: string | null) =>
  iso ? new Date(iso).toLocaleDateString("en-US", { timeZone: "UTC", dateStyle: "medium" }) : "—";

/** Users + joined stats with role change (GET/PATCH /api/admin/users — docs/03 §8.5). */
export function UsersTable({
  initial,
  currentUserId,
}: {
  initial: AdminUser[];
  currentUserId: string;
}) {
  const [users, setUsers] = React.useState(initial);
  const [pending, setPending] = React.useState<string | null>(null);

  async function changeRole(user: AdminUser, role: UserRole) {
    if (!canChangeRole(currentUserId, user, role)) return;
    setPending(user.id);
    try {
      const updated = await api.patchAdminUser(user.id, role);
      setUsers((cur) => cur.map((u) => (u.id === user.id ? { ...u, role: updated.role } : u)));
      toast.success(`${user.name} is now ${role === "admin" ? "an admin" : "a patron"}.`);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Could not change the role.");
    } finally {
      setPending(null);
    }
  }

  return (
    <Card data-testid="users-table">
      <CardHeader className="flex-row items-baseline justify-between space-y-0 pb-3">
        <CardTitle className="text-base">Accounts</CardTitle>
        <span className="text-xs text-muted-foreground">{users.length} users</span>
      </CardHeader>
      <CardContent>
        <div className="overflow-x-auto">
          <table className="w-full text-left text-sm">
            <thead>
              <tr className="border-b border-border text-xs uppercase tracking-wider text-muted-foreground">
                <th className="py-2 pr-4 font-medium">Name</th>
                <th className="py-2 pr-4 font-medium">Email</th>
                <th className="py-2 pr-4 text-right font-medium">Library</th>
                <th className="py-2 pr-4 text-right font-medium">Holds</th>
                <th className="py-2 pr-4 font-medium">Last login</th>
                <th className="py-2 font-medium">Role</th>
              </tr>
            </thead>
            <tbody>
              {users.map((u) => {
                const self = u.id === currentUserId;
                return (
                  <tr
                    key={u.id}
                    className="border-b border-border/60 last:border-0"
                    data-testid="user-row"
                    data-email={u.email}
                  >
                    <td className="py-2.5 pr-4 font-medium">
                      {u.name}
                      {self && (
                        <Badge variant="outline" className="ml-2">
                          you
                        </Badge>
                      )}
                    </td>
                    <td className="py-2.5 pr-4 text-muted-foreground">{u.email}</td>
                    <td className="py-2.5 pr-4 text-right font-mono tabular-nums">
                      {u.library_count}
                    </td>
                    <td className="py-2.5 pr-4 text-right font-mono tabular-nums">
                      {u.waitlist_count}
                    </td>
                    <td className="py-2.5 pr-4 text-xs text-muted-foreground">
                      {dateFmt(u.last_login_at)}
                    </td>
                    <td className="py-2.5">
                      <select
                        aria-label={`Role for ${u.name}`}
                        data-testid="user-role"
                        className={selectClass}
                        value={u.role}
                        disabled={self || pending === u.id}
                        title={self ? "You cannot demote yourself." : undefined}
                        onChange={(e) => void changeRole(u, e.target.value as UserRole)}
                      >
                        <option value="user">Patron</option>
                        <option value="admin">Admin</option>
                      </select>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </CardContent>
    </Card>
  );
}
