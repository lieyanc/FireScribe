import type { ReactNode } from "react";
import { AlertCircle } from "lucide-react";
import { Alert, AlertDescription } from "../ui/alert";
import { Button, type ButtonProps } from "../ui/button";
import { Card, CardContent } from "../ui/card";
import { Tooltip, TooltipContent, TooltipTrigger } from "../ui/tooltip";
import { cn } from "../../lib/utils";

export function PageHeader({
  title,
  description,
  children,
}: {
  title: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <section className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
      <div className="min-w-0 space-y-1">
        <h1 className="truncate text-2xl font-semibold tracking-normal">{title}</h1>
        {description ? <p className="text-sm text-muted-foreground">{description}</p> : null}
      </div>
      {children ? <div className="flex flex-wrap items-center gap-2 md:justify-end">{children}</div> : null}
    </section>
  );
}

export function MetricCard({
  icon,
  label,
  value,
  hint,
}: {
  icon?: ReactNode;
  label: string;
  value: ReactNode;
  hint?: ReactNode;
}) {
  return (
    <Card>
      <CardContent className="flex items-center gap-3 p-4">
        {icon ? <div className="flex size-9 items-center justify-center rounded-md bg-primary/10 text-primary">{icon}</div> : null}
        <div className="min-w-0">
          <div className="text-xs text-muted-foreground">{label}</div>
          <div className="mt-1 truncate text-lg font-semibold leading-none">{value}</div>
          {hint ? <div className="mt-1 truncate text-xs text-muted-foreground">{hint}</div> : null}
        </div>
      </CardContent>
    </Card>
  );
}

export function EmptyState({
  icon,
  title,
  description,
  children,
  className,
}: {
  icon?: ReactNode;
  title: string;
  description?: string;
  children?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex min-h-48 flex-col items-center justify-center gap-3 px-4 py-10 text-center", className)}>
      {icon ? <div className="flex size-11 items-center justify-center rounded-md bg-muted text-muted-foreground">{icon}</div> : null}
      <div className="space-y-1">
        <div className="text-sm font-medium">{title}</div>
        {description ? <div className="max-w-sm text-sm text-muted-foreground">{description}</div> : null}
      </div>
      {children}
    </div>
  );
}

export function ErrorMessage({ message }: { message?: string }) {
  if (!message) return null;
  return (
    <Alert variant="destructive">
      <AlertCircle className="size-4" />
      <AlertDescription>{message}</AlertDescription>
    </Alert>
  );
}

export function IconTooltipButton({
  label,
  children,
  disabled,
  ...props
}: ButtonProps & {
  label: string;
  children: ReactNode;
}) {
  const button = (
    <Button aria-label={label} disabled={disabled} {...props}>
      {children}
    </Button>
  );
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        {disabled ? <span className="inline-flex cursor-not-allowed">{button}</span> : button}
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}
