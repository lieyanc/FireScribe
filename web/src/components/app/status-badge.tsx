import { Badge } from "@/components/ui/badge";
import { statusLabel } from "@/lib/format";

const statusVariants = {
  failed: "destructive",
  canceled: "destructive",
  partial: "warning",
  finalized: "success",
  verified: "success",
  succeeded: "success",
  ready: "success",
  checking: "default",
  downloading: "default",
  applying: "default",
  recognizing: "default",
  reviewing: "default",
  review_pending: "secondary",
  running: "default",
  queued: "secondary",
  open: "secondary",
  resolved: "outline",
  ignored: "outline",
  idle: "outline",
} as const;

export function StatusBadge({ value, className }: { value: string; className?: string }) {
  const variant = statusVariants[value as keyof typeof statusVariants] ?? "outline";
  return (
    <Badge variant={variant} className={className}>
      {statusLabel(value)}
    </Badge>
  );
}
