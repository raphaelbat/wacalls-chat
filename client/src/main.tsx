import { StrictMode, Suspense, useEffect } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { ChatsPage } from "@/pages/ChatsPage";
import { LoginPage } from "@/pages/LoginPage";
import { AdminUsersPage } from "@/pages/AdminUsersPage";
import { ConnectionsPage } from "@/pages/ConnectionsPage";
import ReportsPage from "@/pages/ReportsPage";
import ContactsPage from "@/pages/ContactsPage";
import QueuesPage from "@/pages/QueuesPage";
import KanbanPage from "@/pages/KanbanPage";
import { RequireAuth } from "@/components/auth/RequireAuth";
import { ErrorBoundary } from "@/components/shared/ErrorBoundary";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Toaster } from "@/components/ui/sonner";
import { useTheme } from "@/stores/theme";
import { queryClient } from "@/lib/query";
import { ensureSessionsWired } from "@/stores/sessions";
import { SplashFallback } from "@/components/shared/Splash";
import "@/styles/index.css";
import { applyCachedWhitelabel, loadAndApplyWhitelabel } from "@/lib/whitelabel";
import "@/i18n";

applyCachedWhitelabel();

const Root = () => {
  const theme = useTheme((s) => s.theme);
  useEffect(() => {
    ensureSessionsWired();
    void loadAndApplyWhitelabel();
  }, []);
  return (
    <TooltipProvider delayDuration={200}>
      <BrowserRouter>
        <ErrorBoundary>
          <Suspense fallback={<SplashFallback />}>
            <Routes>
              <Route path="/login" element={<LoginPage />} />
              <Route path="/" element={<RequireAuth><Navigate to="/chats" replace /></RequireAuth>} />
              <Route path="/chats" element={<RequireAuth><ChatsPage /></RequireAuth>} />
              <Route path="/connections" element={<RequireAuth><ConnectionsPage /></RequireAuth>} />
              <Route path="/reports" element={<RequireAuth><ReportsPage /></RequireAuth>} />
              <Route path="/contacts" element={<RequireAuth><ContactsPage /></RequireAuth>} />
              <Route path="/queues" element={<RequireAuth><QueuesPage /></RequireAuth>} />
              <Route path="/kanban" element={<RequireAuth><KanbanPage /></RequireAuth>} />
              <Route path="/admin/users" element={<RequireAuth adminOnly><AdminUsersPage /></RequireAuth>} />
              <Route path="*" element={<Navigate to="/chats" replace />} />
            </Routes>
          </Suspense>
        </ErrorBoundary>
      </BrowserRouter>
      <Toaster theme={theme} position="top-right" richColors closeButton />
    </TooltipProvider>
  );
};

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <Root />
    </QueryClientProvider>
  </StrictMode>,
);
