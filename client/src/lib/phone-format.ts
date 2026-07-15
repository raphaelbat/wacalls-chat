// Best-effort formatting for WhatsApp JIDs/MSISDNs into "+55 (81) 9988-5670".
export const formatPhone = (raw: string): string => {
  if (!raw) return "";
  const digits = raw.split("@")[0].replace(/\D/g, "");
  if (!digits) return raw;
  // BR mobile: 55 + DDD(2) + 9 + 8 digits = 13
  if (digits.startsWith("55") && digits.length === 13) {
    return `+55 (${digits.slice(2, 4)}) ${digits.slice(4, 9)}-${digits.slice(9)}`;
  }
  if (digits.startsWith("55") && digits.length === 12) {
    return `+55 (${digits.slice(2, 4)}) ${digits.slice(4, 8)}-${digits.slice(8)}`;
  }
  // generic: "+<rest>"
  return `+${digits}`;
};
