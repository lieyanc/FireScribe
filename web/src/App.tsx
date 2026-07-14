import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  ArrowLeft,
  BookOpenText,
  FolderKanban,
  FileQuestion,
  ListChecks,
  Moon,
  RefreshCw,
  ScanSearch,
  ChartNoAxesCombined,
  Settings2,
  Sun,
  UserRoundPen,
  type LucideIcon,
} from "lucide-react";
import { lazy, Suspense, useEffect, useState } from "react";
import { Link, Route, Routes, useLocation } from "react-router-dom";
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
import { Toaster } from "./components/ui/sonner";

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

type Theme = "light" | "dark";

type NavigationItem = {
  to: string;
  icon: LucideIcon;
  label: string;
  isActive: (pathname: string) => boolean;
};

const queryClient = new QueryClient();
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
      <AppShell theme={theme} onToggleTheme={() => setTheme((current) => (current === "dark" ? "light" : "dark"))} />
      <Toaster theme={theme} richColors closeButton />
    </QueryClientProvider>
  );
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
        <div className="mx-auto flex w-full max-w-7xl flex-1 flex-col px-4 py-5 md:px-6 md:py-6">
          <Suspense fallback={<div className="flex min-h-[50vh] items-center justify-center text-sm text-muted-foreground">正在加载页面…</div>}>
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
              <Route path="/settings" element={<SettingsPage />} />
              <Route path="/system" element={<SystemPage />} />
              <Route path="*" element={<NotFoundPage />} />
            </Routes>
          </Suspense>
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}

function AppSidebar({ pathname, theme, onToggleTheme }: { pathname: string; theme: Theme; onToggleTheme: () => void }) {
  const { isMobile, setOpenMobile } = useSidebar();

  return (
    <Sidebar collapsible="icon" variant="inset">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton asChild size="lg" tooltip="FireScribe 文档库">
              <Link
                to="/"
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
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
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
