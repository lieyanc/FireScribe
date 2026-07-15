import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  ArrowLeft,
  BookOpenText,
  ChevronsUpDown,
  FolderKanban,
  FileQuestion,
  KeyRound,
  ListChecks,
  LogOut,
  Moon,
  RefreshCw,
  ScanSearch,
  ChartNoAxesCombined,
  Settings2,
  ShieldBan,
  Sun,
  UserRoundPen,
  UsersRound,
  type LucideIcon,
} from "lucide-react";
import { lazy, Suspense, useEffect, useState, type ReactNode } from "react";
import { Link, Route, Routes, useLocation } from "react-router-dom";
import { ChangePasswordDialog } from "./components/app/change-password-dialog";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "./components/ui/breadcrumb";
import { Button } from "./components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "./components/ui/dropdown-menu";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "./components/ui/empty";
import { Separator } from "./components/ui/separator";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarSeparator,
  SidebarTrigger,
  useSidebar,
} from "./components/ui/sidebar";
import { Spinner } from "./components/ui/spinner";
import { Toaster } from "./components/ui/sonner";
import { AuthProvider, useAuth } from "./hooks/use-auth";
import { ApiError } from "./lib/api";
import { LoginPage } from "./pages/LoginPage";

const DocumentsPage = lazy(() => import("./pages/DocumentsPage").then((module) => ({ default: module.DocumentsPage })));
const DocumentDetailPage = lazy(() => import("./pages/DocumentDetailPage").then((module) => ({ default: module.DocumentDetailPage })));
const ReviewPage = lazy(() => import("./pages/ReviewPage").then((module) => ({ default: module.ReviewPage })));
const JobsPage = lazy(() => import("./pages/JobsPage").then((module) => ({ default: module.JobsPage })));
const ProjectsPage = lazy(() => import("./pages/ProjectsPage").then((module) => ({ default: module.ProjectsPage })));
const ProjectDetailPage = lazy(() => import("./pages/ProjectDetailPage").then((module) => ({ default: module.ProjectDetailPage })));
const ReviewQueuePage = lazy(() => import("./pages/ReviewQueuePage").then((module) => ({ default: module.ReviewQueuePage })));
const EvaluationPage = lazy(() => import("./pages/EvaluationPage").then((module) => ({ default: module.EvaluationPage })));
const AuthorsPage = lazy(() => import("./pages/AuthorsPage").then((module) => ({ default: module.AuthorsPage })));
const AuthorDetailPage = lazy(() => import("./pages/AuthorDetailPage").then((module) => ({ default: module.AuthorDetailPage })));
const SettingsPage = lazy(() => import("./pages/SettingsPage").then((module) => ({ default: module.SettingsPage })));
const SystemPage = lazy(() => import("./pages/SystemPage").then((module) => ({ default: module.SystemPage })));
const UsersPage = lazy(() => import("./pages/UsersPage").then((module) => ({ default: module.UsersPage })));

type Theme = "light" | "dark";

type NavigationItem = {
  to: string;
  icon: LucideIcon;
  label: string;
  isActive: (pathname: string) => boolean;
};

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: (failureCount, error) => {
        if (error instanceof ApiError && (error.status === 401 || error.status === 403)) return false;
        return failureCount < 3;
      },
    },
  },
});
const themeStorageKey = "firescribe-theme";

const primaryNavigation: NavigationItem[] = [
  {
    to: "/",
    icon: BookOpenText,
    label: "文档",
    isActive: (pathname) =>
      pathname === "/" || pathname.startsWith("/documents/") || pathname.startsWith("/review/"),
  },
  {
    to: "/jobs",
    icon: ListChecks,
    label: "任务",
    isActive: (pathname) => pathname === "/jobs",
  },
  {
    to: "/projects",
    icon: FolderKanban,
    label: "项目",
    isActive: (pathname) => pathname === "/projects" || pathname.startsWith("/projects/"),
  },
  {
    to: "/review-queue",
    icon: ScanSearch,
    label: "低置信",
    isActive: (pathname) => pathname === "/review-queue",
  },
  {
    to: "/authors",
    icon: UserRoundPen,
    label: "作者",
    isActive: (pathname) => pathname === "/authors" || pathname.startsWith("/authors/"),
  },
  {
    to: "/evaluation",
    icon: ChartNoAxesCombined,
    label: "评测",
    isActive: (pathname) => pathname === "/evaluation",
  },
];

const managementNavigation: NavigationItem[] = [
  {
    to: "/settings",
    icon: Settings2,
    label: "设置",
    isActive: (pathname) => pathname === "/settings",
  },
  {
    to: "/users",
    icon: UsersRound,
    label: "用户",
    isActive: (pathname) => pathname === "/users",
  },
  {
    to: "/system",
    icon: RefreshCw,
    label: "系统",
    isActive: (pathname) => pathname === "/system",
  },
];

export function App() {
  const [theme, setTheme] = useState<Theme>(() => {
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
      <AuthProvider>
        <AuthGate theme={theme} onToggleTheme={() => setTheme((current) => (current === "dark" ? "light" : "dark"))} />
      </AuthProvider>
      <Toaster theme={theme} richColors closeButton />
    </QueryClientProvider>
  );
}

function AuthGate({ theme, onToggleTheme }: { theme: Theme; onToggleTheme: () => void }) {
  const { state } = useAuth();

  if (state.phase === "loading") {
    return (
      <div className="flex min-h-svh items-center justify-center gap-2 text-sm text-muted-foreground">
        <Spinner />
        正在检查登录状态…
      </div>
    );
  }
  if (state.phase === "setup") {
    return <LoginPage mode="setup" />;
  }
  if (state.phase === "guest") {
    return <LoginPage mode="login" />;
  }
  return <AppShell theme={theme} onToggleTheme={onToggleTheme} />;
}

function AppShell({ theme, onToggleTheme }: { theme: Theme; onToggleTheme: () => void }) {
  const { pathname } = useLocation();

  return (
    <SidebarProvider>
      <AppSidebar pathname={pathname} theme={theme} onToggleTheme={onToggleTheme} />
      <SidebarInset className="min-w-0">
        <header className="sticky top-0 flex h-14 shrink-0 items-center gap-2 border-b bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/80">
          <SidebarTrigger aria-label="切换导航栏" title="切换导航栏" />
          <Separator orientation="vertical" className="h-4" />
          <RouteBreadcrumb pathname={pathname} />
        </header>
        <div className="mx-auto flex w-full max-w-7xl flex-1 flex-col px-4 py-5 md:px-6 md:py-6 [view-transition-name:main-content]">
          <Suspense
            fallback={
              <div className="flex min-h-[50vh] items-center justify-center text-sm text-muted-foreground animate-in fade-in duration-300 delay-150 fill-mode-backwards">
                正在加载页面…
              </div>
            }
          >
            <Routes>
              <Route path="/" element={<DocumentsPage />} />
              <Route path="/documents/:documentID" element={<DocumentDetailPage />} />
              <Route path="/review/:documentID" element={<ReviewPage />} />
              <Route path="/review/:documentID/:pageID" element={<ReviewPage />} />
              <Route path="/jobs" element={<JobsPage />} />
              <Route path="/projects" element={<ProjectsPage />} />
              <Route path="/projects/:projectID" element={<ProjectDetailPage />} />
              <Route path="/review-queue" element={<ReviewQueuePage />} />
              <Route path="/evaluation" element={<EvaluationPage />} />
              <Route path="/authors" element={<AuthorsPage />} />
              <Route path="/authors/:profileID" element={<AuthorDetailPage />} />
              <Route
                path="/settings"
                element={
                  <AdminRoute>
                    <SettingsPage />
                  </AdminRoute>
                }
              />
              <Route
                path="/users"
                element={
                  <AdminRoute>
                    <UsersPage />
                  </AdminRoute>
                }
              />
              <Route
                path="/system"
                element={
                  <AdminRoute>
                    <SystemPage />
                  </AdminRoute>
                }
              />
              <Route path="*" element={<NotFoundPage />} />
            </Routes>
          </Suspense>
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}

function AdminRoute({ children }: { children: ReactNode }) {
  const { isAdmin } = useAuth();
  if (!isAdmin) {
    return (
      <Empty className="min-h-[60vh] border">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ShieldBan />
          </EmptyMedia>
          <EmptyTitle>需要管理员权限</EmptyTitle>
          <EmptyDescription>该页面仅对管理员开放。如需修改设置或管理用户，请联系管理员。</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button asChild>
            <Link to="/">
              <ArrowLeft />
              返回文档库
            </Link>
          </Button>
        </EmptyContent>
      </Empty>
    );
  }
  return <>{children}</>;
}

function AppSidebar({ pathname, theme, onToggleTheme }: { pathname: string; theme: Theme; onToggleTheme: () => void }) {
  const { isMobile, setOpenMobile } = useSidebar();
  const { isAdmin } = useAuth();

  return (
    <Sidebar collapsible="icon" variant="inset">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton asChild size="lg" tooltip="FireScribe 文档库">
              <Link
                to="/"
                viewTransition
                aria-label="返回文档库"
                onClick={() => {
                  if (isMobile) setOpenMobile(false);
                }}
              >
                <BookOpenText />
                <span>FireScribe</span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      <SidebarSeparator />

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>工作区</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              {primaryNavigation.map((item) => (
                <AppNavigationItem key={item.to} item={item} pathname={pathname} />
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarSeparator />

      <SidebarFooter>
        {isAdmin ? (
          <SidebarGroup className="p-0">
            <SidebarGroupLabel>管理</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {managementNavigation.map((item) => (
                  <AppNavigationItem key={item.to} item={item} pathname={pathname} />
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ) : null}

        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              type="button"
              tooltip={theme === "dark" ? "切换为浅色模式" : "切换为深色模式"}
              aria-label={theme === "dark" ? "切换为浅色模式" : "切换为深色模式"}
              onClick={onToggleTheme}
            >
              {theme === "dark" ? <Sun /> : <Moon />}
              <span>{theme === "dark" ? "浅色模式" : "深色模式"}</span>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarUserMenu />
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}

function SidebarUserMenu() {
  const { user, isAdmin, signOut } = useAuth();
  const { isMobile } = useSidebar();
  const [changePasswordOpen, setChangePasswordOpen] = useState(false);

  if (!user) return null;
  const label = user.display_name || user.username;

  return (
    <SidebarMenuItem>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <SidebarMenuButton size="lg" tooltip={label} aria-label="账户菜单">
            <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-primary text-xs font-semibold text-primary-foreground">
              {label.slice(0, 1).toUpperCase()}
            </span>
            <span className="flex min-w-0 flex-col leading-tight">
              <span className="truncate font-medium">{label}</span>
              <span className="truncate text-xs text-muted-foreground">{isAdmin ? "管理员" : "普通用户"}</span>
            </span>
            <ChevronsUpDown className="ml-auto size-4" />
          </SidebarMenuButton>
        </DropdownMenuTrigger>
        <DropdownMenuContent side={isMobile ? "bottom" : "right"} align="end" className="min-w-48">
          <DropdownMenuLabel className="flex flex-col">
            <span>{label}</span>
            <span className="text-xs font-normal text-muted-foreground">@{user.username}</span>
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => setChangePasswordOpen(true)}>
            <KeyRound />
            修改密码
          </DropdownMenuItem>
          <DropdownMenuItem className="text-destructive focus:text-destructive" onSelect={() => void signOut()}>
            <LogOut />
            退出登录
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <ChangePasswordDialog open={changePasswordOpen} onOpenChange={setChangePasswordOpen} />
    </SidebarMenuItem>
  );
}

function AppNavigationItem({ item, pathname }: { item: NavigationItem; pathname: string }) {
  const { isMobile, setOpenMobile } = useSidebar();
  const active = item.isActive(pathname);
  const Icon = item.icon;

  return (
    <SidebarMenuItem>
      <SidebarMenuButton asChild isActive={active} tooltip={item.label}>
        <Link
          to={item.to}
          viewTransition
          aria-current={active ? "page" : undefined}
          onClick={() => {
            if (isMobile) setOpenMobile(false);
          }}
        >
          <Icon />
          <span>{item.label}</span>
        </Link>
      </SidebarMenuButton>
    </SidebarMenuItem>
  );
}

function RouteBreadcrumb({ pathname }: { pathname: string }) {
  const section = getRouteSection(pathname);

  return (
    <Breadcrumb className="min-w-0">
      <BreadcrumbList className="flex-nowrap">
        {section.parent ? (
          <>
            <BreadcrumbItem>
              <BreadcrumbLink asChild>
                <Link to={section.parent.to}>{section.parent.label}</Link>
              </BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
          </>
        ) : null}
        <BreadcrumbItem className="min-w-0">
          <BreadcrumbPage className="truncate">{section.label}</BreadcrumbPage>
        </BreadcrumbItem>
      </BreadcrumbList>
    </Breadcrumb>
  );
}

function getRouteSection(pathname: string): { label: string; parent?: { label: string; to: string } } {
  if (pathname === "/") return { label: "文档库" };
  if (pathname.startsWith("/documents/")) {
    return { label: "文档详情", parent: { label: "文档库", to: "/" } };
  }
  if (pathname.startsWith("/review/")) {
    return { label: "校对", parent: { label: "文档库", to: "/" } };
  }
  if (pathname === "/jobs") return { label: "任务" };
  if (pathname === "/projects") return { label: "项目" };
  if (pathname.startsWith("/projects/")) return { label: "项目详情", parent: { label: "项目", to: "/projects" } };
  if (pathname === "/review-queue") return { label: "低置信队列" };
  if (pathname === "/evaluation") return { label: "识别评测" };
  if (pathname === "/authors") return { label: "作者档案" };
  if (pathname.startsWith("/authors/")) return { label: "档案详情", parent: { label: "作者档案", to: "/authors" } };
  if (pathname === "/settings") return { label: "设置" };
  if (pathname === "/users") return { label: "用户" };
  if (pathname === "/system") return { label: "系统" };
  return { label: "页面未找到" };
}

function NotFoundPage() {
  return (
    <Empty className="min-h-[60vh] border">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <FileQuestion />
        </EmptyMedia>
        <EmptyTitle>页面未找到</EmptyTitle>
        <EmptyDescription>你访问的页面不存在，可能已被移动或链接有误。</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button asChild>
          <Link to="/">
            <ArrowLeft />
            返回文档库
          </Link>
        </Button>
      </EmptyContent>
    </Empty>
  );
}
