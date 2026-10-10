import { Skeleton } from "@/components/ui/skeleton";

/** Section skeleton for the result grid — no spinners (docs/07 §9). */
export function ResultsSkeleton() {
  return (
    <div className="flex flex-col gap-4" aria-busy="true" data-testid="catalog-skeleton">
      <Skeleton className="h-4 w-24" />
      <div className="grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-4">
        {Array.from({ length: 8 }).map((_, i) => (
          <Skeleton key={i} className="aspect-[2/3] w-full" />
        ))}
      </div>
    </div>
  );
}
