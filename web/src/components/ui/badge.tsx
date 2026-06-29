import { cn, statusLabel } from "../../lib/utils";

const tone: Record<string, string> = {
  failed: "border-destructive/20 bg-destructive/10 text-destructive",
  canceled: "border-destructive/20 bg-destructive/10 text-destructive",
  checking: "border-primary/25 bg-primary/10 text-primary",
  downloading: "border-primary/25 bg-primary/10 text-primary",
  reviewing: "border-primary/25 bg-primary/10 text-primary",
  ready: "border-primary/25 bg-primary/10 text-primary",
  finalized: "border-primary/25 bg-primary/10 text-primary",
  verified: "border-primary/25 bg-primary/10 text-primary",
  succeeded: "border-primary/25 bg-primary/10 text-primary",
  applying: "border-accent bg-accent text-accent-foreground",
  recognizing: "border-accent bg-accent text-accent-foreground",
  running: "border-accent bg-accent text-accent-foreground",
  queued: "border-accent bg-accent text-accent-foreground",
  open: "border-accent bg-accent text-accent-foreground",
  resolved: "border-border bg-secondary text-secondary-foreground",
};

export function Badge({ value, className }: { value: string; className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex h-6 items-center rounded-md border px-2.5 text-xs font-semibold transition-colors",
        tone[value] ?? "border-transparent bg-secondary text-secondary-foreground",
        className,
      )}
    >
      {statusLabel(value)}
    </span>
  );
}
