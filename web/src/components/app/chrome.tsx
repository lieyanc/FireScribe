import type { ReactNode } from "react";
import { AlertCircle } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button, type ButtonProps } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { Spinner } from "@/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

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
    <section className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
      <div className="flex min-w-0 flex-col gap-1">
        <h1 className="truncate text-2xl font-semibold tracking-tight">{title}</h1>
        {description ? <p className="max-w-3xl text-sm text-muted-foreground">{description}</p> : null}
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
      <CardHeader className="flex-row items-start justify-between gap-3 pb-2">
        <div className="flex min-w-0 flex-col gap-1">
          <CardDescription>{label}</CardDescription>
          <CardTitle className="truncate text-xl tabular-nums">{value}</CardTitle>
        </div>
        {icon ? (
          <div className="flex size-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground [&_svg]:size-4">
            {icon}
          </div>
        ) : null}
      </CardHeader>
      {hint ? <CardContent className="truncate pb-4 text-xs text-muted-foreground">{hint}</CardContent> : null}
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
    <Empty className={cn("min-h-48 border-0", className)}>
      <EmptyHeader>
        {icon ? <EmptyMedia variant="icon">{icon}</EmptyMedia> : null}
        <EmptyTitle>{title}</EmptyTitle>
        {description ? <EmptyDescription>{description}</EmptyDescription> : null}
      </EmptyHeader>
      {children ? <EmptyContent>{children}</EmptyContent> : null}
    </Empty>
  );
}

export function ErrorMessage({
  message,
  title = "操作未完成",
  onRetry,
}: {
  message?: string;
  title?: string;
  onRetry?: () => void;
}) {
  if (!message) return null;
  return (
    <Alert variant="destructive">
      <AlertCircle />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <span className="break-words">{message}</span>
        {onRetry ? (
          <Button type="button" variant="outline" size="sm" onClick={onRetry}>
            重试
          </Button>
        ) : null}
      </AlertDescription>
    </Alert>
  );
}

export function PendingButton({
  pending,
  pendingLabel,
  icon,
  children,
  disabled,
  ...props
}: ButtonProps & {
  pending: boolean;
  pendingLabel?: ReactNode;
  icon?: ReactNode;
}) {
  return (
    <Button disabled={pending || disabled} {...props}>
      {pending ? <Spinner /> : icon}
      {pending && pendingLabel != null ? pendingLabel : children}
    </Button>
  );
}

export function LabeledValue({
  label,
  value,
  className,
  title,
}: {
  label: string;
  value: ReactNode;
  className?: string;
  title?: string;
}) {
  return (
    <div className="min-w-0">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={cn("mt-1 truncate", className)} title={title}>
        {value}
      </div>
    </div>
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
        {disabled ? (
          <span className="inline-flex cursor-not-allowed" tabIndex={0} aria-label={label}>
            {button}
          </span>
        ) : (
          button
        )}
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}
