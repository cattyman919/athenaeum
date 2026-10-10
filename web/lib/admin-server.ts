import "server-only";
import { apiServer } from "./api-server";

/** Server-component GET of an admin endpoint; null on any failure (rendered as error state). */
export async function adminGet<T>(path: string): Promise<T | null> {
  try {
    const res = await apiServer(path);
    return res.ok ? ((await res.json()) as T) : null;
  } catch {
    return null;
  }
}
