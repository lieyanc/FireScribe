import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { NavLink, Route, Routes } from "react-router-dom";
import { BookOpenText, BriefcaseBusiness, ListChecks, Moon, RefreshCw, Sun } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { DocumentsPage } from "./pages/DocumentsPage";
import { DocumentDetailPage } from "./pages/DocumentDetailPage";
import { ReviewPage } from "./pages/ReviewPage";
import { JobsPage } from "./pages/JobsPage";
import { SystemPage } from "./pages/SystemPage";
import { cn } from "./lib/utils";
import { Button } from "./components/ui/button";
import { Separator } from "./components/ui/separator";
import { TooltipProvider } from "./components/ui/tooltip";

const queryClient = new QueryClient();
const themeStorageKey = "firescribe-theme";
const navItems = [
  { to: "/", icon: <BriefcaseBusiness className="size-4" />, label: "文档" },
  { to: "/jobs", icon: <ListChecks className="size-4" />, label: "任务" },
  { to: "/system", icon: <RefreshCw className="size-4" />, label: "系统" },
];

export function App() {
  const [theme, setTheme] = useState<"light" | "dark">(() => {
    if (typeof window === "undefined") return "light";
    const stored = window.localStorage.getItem(themeStorageKey);
    if (stored === "light" || stored === "dark") return stored;
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  });

  useEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark");
    window.localStorage.setItem(themeStorageKey, theme);
  }, [theme]);

  return (
    <QueryClientProvider client={queryClient}>
      <TooltipProvider delayDuration={200}>
        <div className="min-h-screen bg-background">
          <div className="flex min-h-screen">
            <aside className="hidden w-52 shrink-0 border-r bg-muted/25 min-[560px]:flex min-[560px]:flex-col lg:w-60">
              <div className="flex h-14 items-center gap-2 px-4">
                <Brand />
              </div>
              <Separator />
              <nav className="flex flex-1 flex-col gap-1 p-2">
                {navItems.map((item) => (
                  <NavItem key={item.to} {...item} />
                ))}
              </nav>
              <div className="border-t p-3">
                <ThemeToggle theme={theme} setTheme={setTheme} />
              </div>
            </aside>

            <div className="flex min-w-0 flex-1 flex-col">
              <header className="sticky top-0 z-40 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/80 min-[560px]:hidden">
                <div className="flex h-14 items-center justify-between px-3">
                  <Brand />
                  <div className="flex items-center gap-1">
                    <nav className="flex items-center gap-1">
                      {navItems.map((item) => (
                        <NavItem key={item.to} {...item} compact />
                      ))}
                    </nav>
                    <ThemeToggle theme={theme} setTheme={setTheme} compact />
                  </div>
                </div>
              </header>
              <main className="mx-auto w-full max-w-7xl flex-1 px-3 py-4 sm:px-5 lg:px-6 lg:py-6">
                <Routes>
                  <Route path="/" element={<DocumentsPage />} />
                  <Route path="/documents/:documentID" element={<DocumentDetailPage />} />
                  <Route path="/review/:documentID" element={<ReviewPage />} />
                  <Route path="/review/:documentID/:pageID" element={<ReviewPage />} />
                  <Route path="/jobs" element={<JobsPage />} />
                  <Route path="/system" element={<SystemPage />} />
                </Routes>
              </main>
            </div>
          </div>
        </div>
      </TooltipProvider>
    </QueryClientProvider>
  );
}

function Brand() {
  return (
    <NavLink to="/" className="flex min-w-0 items-center gap-2 font-semibold">
      <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-primary text-primary-foreground shadow-sm">
        <BookOpenText className="size-4" />
      </span>
      <span className="truncate">FireScribe</span>
    </NavLink>
  );
}

function ThemeToggle({
  theme,
  setTheme,
  compact,
}: {
  theme: "light" | "dark";
  setTheme: (updater: (value: "light" | "dark") => "light" | "dark") => void;
  compact?: boolean;
}) {
  return (
    <Button
      variant={compact ? "ghost" : "outline"}
      size={compact ? "icon" : "sm"}
      className={cn(!compact && "w-full justify-start")}
      title={theme === "dark" ? "浅色" : "深色"}
      aria-label={theme === "dark" ? "切换浅色模式" : "切换深色模式"}
      onClick={() => setTheme((value) => (value === "dark" ? "light" : "dark"))}
    >
      {theme === "dark" ? <Sun /> : <Moon />}
      {!compact ? <span>{theme === "dark" ? "浅色" : "深色"}</span> : null}
    </Button>
  );
}

function NavItem({ to, icon, label, compact }: { to: string; icon: ReactNode; label: string; compact?: boolean }) {
  return (
    <NavLink
      to={to}
      aria-label={label}
      className={({ isActive }) =>
        cn(
          "inline-flex h-9 items-center gap-2.5 rounded-md px-3 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring",
          isActive
            ? "bg-accent text-accent-foreground shadow-sm"
            : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
          compact && "size-9 justify-center gap-2 px-0",
        )
      }
    >
      {icon}
      <span className={compact ? "sr-only" : undefined}>{label}</span>
    </NavLink>
  );
}
