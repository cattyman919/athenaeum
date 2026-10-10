import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const alertVariants = cva("relative flex w-full gap-3 rounded-lg border p-4 text-sm", {
  variants: {
    variant: {
      success: "border-status-available-dot/30 bg-status-available-bg text-status-available-fg",
      warning: "border-status-out-dot/30 bg-status-out-bg text-status-out-fg",
      destructive: "border-status-na-dot/30 bg-status-na-bg text-status-na-fg",
    },
  },
  defaultVariants: { variant: "success" },
});

function Alert({
  className,
  variant,
  ...props
}: React.HTMLAttributes<HTMLDivElement> & VariantProps<typeof alertVariants>) {
  return <div role="status" className={cn(alertVariants({ variant }), className)} {...props} />;
}

function AlertTitle({ className, ...props }: React.HTMLAttributes<HTMLHeadingElement>) {
  return <h2 className={cn("font-semibold leading-tight", className)} {...props} />;
}

function AlertDescription({ className, ...props }: React.HTMLAttributes<HTMLParagraphElement>) {
  return <p className={cn("mt-1 text-xs opacity-90", className)} {...props} />;
}

export { Alert, AlertTitle, AlertDescription };
