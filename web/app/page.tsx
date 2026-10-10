import { redirect } from "next/navigation";

// middleware.ts sends unauthenticated visitors to /login (docs/07 §2).
export default function Home() {
  redirect("/catalog");
}
