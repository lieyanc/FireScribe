import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { NavLink, Route, Routes } from "react-router-dom";
import { BookOpenText, BriefcaseBusiness, ListChecks, RefreshCw } from "lucide-react";
import type { ReactNode } from "react";
import { DocumentsPage } from "./pages/DocumentsPage";
import { DocumentDetailPage } from "./pages/DocumentDetailPage";
import { ReviewPage } from "./pages/ReviewPage";
import { JobsPage } from "./pages/JobsPage";
import { SystemPage } from "./pages/SystemPage";
import { cn } from "./lib/utils";

const queryClient = new QueryClient();

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <div className="min-h-screen">
        <header className="border-b border-border bg-white/85">
          <div className="mx-auto flex h-14 max-w-7xl items-center justify-between px-4">
            <NavLink to="/" className="flex items-center gap-2 font-semibold">
              <BookOpenText className="h-5 w-5 text-primary" />
              <span>FireScribe</span>
            </NavLink>
            <nav className="flex items-center gap-1">
              <NavItem to="/" icon={<BriefcaseBusiness className="h-4 w-4" />} label="文档" />
              <NavItem to="/jobs" icon={<ListChecks className="h-4 w-4" />} label="任务" />
              <NavItem to="/system" icon={<RefreshCw className="h-4 w-4" />} label="系统" />
            </nav>
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
      className={({ isActive }) =>
        cn(
          "inline-flex h-9 items-center gap-2 rounded-md px-3 text-sm transition",
          isActive ? "bg-muted text-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground",
        )
      }
    >
      {icon}
      {label}
    </NavLink>
  );
}
