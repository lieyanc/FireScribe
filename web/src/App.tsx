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

const queryClient = new QueryClient();
const themeStorageKey = "firescribe-theme";

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
      <div className="min-h-screen bg-background">
        <header className="sticky top-0 z-40 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/80">
          <div className="mx-auto flex h-14 max-w-7xl items-center justify-between px-4">
            <NavLink to="/" className="flex items-center gap-2 font-semibold">
              <BookOpenText className="h-5 w-5 text-primary" />
              <span>FireScribe</span>
            </NavLink>
            <div className="flex items-center gap-1">
              <nav className="flex items-center gap-1">
                <NavItem to="/" icon={<BriefcaseBusiness className="h-4 w-4" />} label="文档" />
                <NavItem to="/jobs" icon={<ListChecks className="h-4 w-4" />} label="任务" />
                <NavItem to="/system" icon={<RefreshCw className="h-4 w-4" />} label="系统" />
              </nav>
              <Button
                variant="ghost"
                size="icon"
                title={theme === "dark" ? "浅色" : "深色"}
                aria-label={theme === "dark" ? "切换浅色模式" : "切换深色模式"}
                onClick={() => setTheme((value) => (value === "dark" ? "light" : "dark"))}
              >
                {theme === "dark" ? <Sun /> : <Moon />}
              </Button>
            </div>
          </div>
        </header>
        <main className="mx-auto max-w-7xl px-4 py-5">
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
    </QueryClientProvider>
  );
}

function NavItem({ to, icon, label }: { to: string; icon: ReactNode; label: string }) {
  return (
    <NavLink
      to={to}
      aria-label={label}
      className={({ isActive }) =>
        cn(
          "inline-flex h-9 items-center gap-2 rounded-md px-2 text-sm transition sm:px-3",
          isActive ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
        )
      }
    >
      {icon}
      <span className="hidden sm:inline">{label}</span>
    </NavLink>
  );
}
