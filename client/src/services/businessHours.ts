import { apiGet, apiPut } from "@/lib/api";

export type BusinessHoursDay = {
  weekday: number; // 0 = domingo
  enabled: boolean;
  open: string; // "08:00"
  close: string; // "18:00"
};

export type BusinessHoursConfig = {
  scope: "session" | "queue";
  scopeId: string;
  enabled: boolean;
  timezone: string;
  message: string;
  days: BusinessHoursDay[];
  holidays: string[];
  updatedAt?: number;
};

export const getBusinessHours = (scope: "session" | "queue", id: string) =>
  apiGet<BusinessHoursConfig>(`/api/business-hours?scope=${scope}&id=${encodeURIComponent(id)}`);

export const saveBusinessHours = (cfg: BusinessHoursConfig) =>
  apiPut<BusinessHoursConfig>("/api/business-hours", cfg);
