import { useQuery } from "@tanstack/react-query";
import { fetchHistory } from "@/services/history";

export const useHistory = (sid: string | null, enabled: boolean) =>
  useQuery({
    queryKey: ["history", sid],
    queryFn: () => fetchHistory(sid as string),
    enabled: enabled && !!sid,
  });
