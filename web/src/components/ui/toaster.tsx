import { useEffect, useState } from "react";
import { AlertCircle, CheckCircle2, Info, X } from "lucide-react";
import { cn } from "../../lib/utils";

type ToastVariant = "default" | "success" | "error";

type ToastItem = {
  id: number;
  title?: string;
  description?: string;
  variant: ToastVariant;
};

let counter = 0;
let items: ToastItem[] = [];
const listeners = new Set<(next: ToastItem[]) => void>();

function emit() {
  for (const listener of listeners) listener(items);
}

function dismiss(id: number) {
  items = items.filter((item) => item.id !== id);
  emit();
}

export function toast(input: {
  title?: string;
  description?: string;
  variant?: ToastVariant;
  duration?: number;
}) {
  const id = ++counter;
  items = [...items, { id, title: input.title, description: input.description, variant: input.variant ?? "default" }];
  emit();
  window.setTimeout(() => dismiss(id), input.duration ?? 4000);
  return id;
}

const variantStyles: Record<ToastVariant, { icon: typeof Info; tone: string }> = {
  default: { icon: Info, tone: "text-foreground" },
  success: { icon: CheckCircle2, tone: "text-primary" },
  error: { icon: AlertCircle, tone: "text-destructive" },
};

export function Toaster() {
  const [current, setCurrent] = useState<ToastItem[]>(items);
  useEffect(() => {
    const listener = (next: ToastItem[]) => setCurrent(next);
    listeners.add(listener);
    setCurrent(items);
    return () => {
      listeners.delete(listener);
    };
  }, []);

  return (
    <div className="pointer-events-none fixed bottom-4 right-4 z-[100] flex w-full max-w-sm flex-col gap-2">
      {current.map((item) => {
        const { icon: Icon, tone } = variantStyles[item.variant];
        return (
          <div
            key={item.id}
            className="pointer-events-auto flex items-start gap-2.5 rounded-lg border bg-background p-3 shadow-lg"
            role="status"
          >
            <Icon className={cn("mt-0.5 size-4 shrink-0", tone)} />
            <div className="min-w-0 flex-1 space-y-0.5">
              {item.title ? <div className="text-sm font-medium">{item.title}</div> : null}
              {item.description ? (
                <div className="break-words text-sm text-muted-foreground">{item.description}</div>
              ) : null}
            </div>
            <button
              type="button"
              aria-label="关闭"
              className="shrink-0 rounded-md p-0.5 text-muted-foreground transition-colors hover:text-foreground"
              onClick={() => dismiss(item.id)}
            >
              <X className="size-4" />
            </button>
          </div>
        );
      })}
    </div>
  );
}
