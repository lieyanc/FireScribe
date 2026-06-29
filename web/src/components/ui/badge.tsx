import { cn, statusLabel } from "../../lib/utils";

const tone: Record<string, string> = {
  failed: "border-red-200 bg-red-50 text-red-700",
  recognizing: "border-amber-200 bg-amber-50 text-amber-800",
  running: "border-amber-200 bg-amber-50 text-amber-800",
  reviewing: "border-sky-200 bg-sky-50 text-sky-800",
  ready: "border-emerald-200 bg-emerald-50 text-emerald-800",
  finalized: "border-teal-200 bg-teal-50 text-teal-800",
  verified: "border-teal-200 bg-teal-50 text-teal-800",
  succeeded: "border-teal-200 bg-teal-50 text-teal-800",
  open: "border-amber-200 bg-amber-50 text-amber-800",
  resolved: "border-teal-200 bg-teal-50 text-teal-800",
};

export function Badge({ value, className }: { value: string; className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex h-6 items-center rounded-md border px-2 text-xs font-medium",
        tone[value] ?? "border-border bg-muted text-muted-foreground",
        className,
      )}
    >
      {statusLabel(value)}
    </span>
  );
}
