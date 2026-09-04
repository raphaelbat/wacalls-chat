import { QueryClient } from "@tanstack/react-query";

export const queryKeys = {
  history: ["history"] as const,
};

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      staleTime: 10_000,
    },
  },
});
