import { useEffect, useState, useRef } from "react";
import { Loader2, Palette, Save, Upload, Trash2, Eye, Layout, Monitor, Smartphone, RotateCcw } from "lucide-react";
import { toast } from "sonner";
import { useTranslation } from "react-i18next";
import { AppShell } from "@/components/layout/AppShell";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import * as settingsApi from "@/services/settings";
import { emitWhitelabelChanged, idealForegroundHex } from "@/lib/whitelabel";
import { useTheme } from "@/stores/theme";
import { ConfirmDialog } from "@/components/shared/ConfirmDialog";

export const WhitelabelSettingsPage = () => {
  const { t } = useTranslation();
  const theme = useTheme((s) => s.theme);
  const isDark = theme === "dark" || (typeof document !== "undefined" && document.documentElement.classList.contains("dark"));

  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [wl, setWl] = useState<settingsApi.Whitelabel>({});
  const [uploadingKind, setUploadingKind] = useState<string | null>(null);
  const [restoring, setRestoring] = useState(false);

  useEffect(() => {
    let alive = true;
    settingsApi
      .getWhitelabel()
      .then((data) => {
        if (alive) setWl(data || {});
      })
      .catch((e) => toast.error(e instanceof Error ? e.message : "Error loading whitelabel"))
      .finally(() => alive && setLoading(false));
    return () => { alive = false; };
  }, []);

  const save = async () => {
    setSaving(true);
    try {
      const updated = await settingsApi.saveWhitelabel(wl);
      setWl(updated);
      emitWhitelabelChanged(updated);
      toast.success(t("pages.whitelabel.saved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Error saving");
    } finally {
      setSaving(false);
    }
  };

  const handleRestore = async () => {
    setSaving(true);
    try {
      const emptyWl: settingsApi.Whitelabel = {
        appName: "",
        appNameMobile: "",
        primaryLight: "",
        primaryDark: "",
        iconColorLight: "",
        iconColorDark: "",
        logoLight: "",
        logoDark: "",
        favicon: "",
        splash: "",
        logoMobile: "",
        bgLight: "",
        bgDark: ""
      };
      const updated = await settingsApi.saveWhitelabel(emptyWl);
      setWl(updated);
      emitWhitelabelChanged(updated);
      toast.success(t("pages.whitelabel.saved"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Error restoring defaults");
    } finally {
      setSaving(false);
      setRestoring(false);
    }
  };

  const handleUpload = async (kind: keyof settingsApi.Whitelabel, file: File) => {
    setUploadingKind(kind);
    try {
      const { url } = await settingsApi.uploadWhitelabelAsset(kind, file);
      const newWl = { ...wl, [kind]: url };
      setWl(newWl);
      toast.success(t("pages.whitelabel.uploadSuccess"));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Upload failed");
    } finally {
      setUploadingKind(null);
    }
  };

  const removeAsset = (kind: keyof settingsApi.Whitelabel) => {
    setWl(prev => ({ ...prev, [kind]: "" }));
  };

  const updateColor = (
    kind: "primaryLight" | "primaryDark" | "iconColorLight" | "iconColorDark",
    val: string,
  ) => {
    setWl(prev => ({ ...prev, [kind]: val }));
  };

  if (loading) {
    return (
      <AppShell>
        <div className="flex h-[200px] items-center justify-center">
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
        </div>
      </AppShell>
    );
  }

  const previewPrimary = (isDark ? wl.primaryDark : wl.primaryLight) || wl.primaryLight || wl.primaryDark || "#4E93FF";
  const previewPrimaryText = idealForegroundHex(previewPrimary);
  const previewIconColor =
    (isDark ? wl.iconColorDark : wl.iconColorLight) || wl.iconColorLight || wl.iconColorDark || previewPrimary;
  const previewLogo = (isDark ? wl.logoDark : wl.logoLight) || wl.logoLight || wl.logoDark;
  const previewName = wl.appName || "VozZap";
  const previewSplash = wl.splash || previewLogo || "/favicon.png";

  return (
    <AppShell>
      <div className="mx-auto w-full max-w-6xl p-4 sm:p-6 space-y-6">
        <header className="flex flex-col gap-1 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h1 className="text-2xl font-bold tracking-tight">{t("pages.whitelabel.title")}</h1>
            <p className="text-muted-foreground">{t("pages.whitelabel.subtitle")}</p>
          </div>
          <Button 
            variant="outline" 
            size="sm" 
            className="w-fit"
            onClick={() => setRestoring(true)}
            disabled={saving}
          >
            <RotateCcw className="mr-2 h-4 w-4" />
            {t("pages.whitelabel.restore", { defaultValue: "Restaurar Padrões" })}
          </Button>
        </header>

        <div className="grid gap-6 lg:grid-cols-3">
          <div className="lg:col-span-2 space-y-6">
            <div className="grid gap-6 md:grid-cols-2">
              {/* Nome e Identidade */}
              <Card>
                <CardHeader>
                  <CardTitle>{t("pages.whitelabel.identityTitle")}</CardTitle>
                  <CardDescription>{t("pages.whitelabel.identityDesc")}</CardDescription>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="space-y-2">
                    <Label htmlFor="appName">{t("pages.whitelabel.appName")}</Label>
                    <Input
                      id="appName"
                      value={wl.appName || ""}
                      onChange={(e) => setWl({ ...wl, appName: e.target.value })}
                      placeholder="VozZap Chat"
                    />
                  </div>
                </CardContent>
              </Card>

              {/* Cores */}
              <Card>
                <CardHeader>
                  <CardTitle>{t("pages.whitelabel.colorsTitle")}</CardTitle>
                  <CardDescription>{t("pages.whitelabel.colorsDesc")}</CardDescription>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="grid grid-cols-2 gap-4">
                    <div className="space-y-2">
                      <Label>{t("pages.whitelabel.primaryLight")}</Label>
                      <div className="flex items-center gap-2">
                        <Input
                          type="color"
                          className="h-10 w-12 p-1 cursor-pointer"
                          value={wl.primaryLight || "#4E93FF"}
                          onChange={(e) => updateColor("primaryLight", e.target.value)}
                        />
                        <Input
                          className="font-mono text-xs"
                          value={wl.primaryLight || "#4E93FF"}
                          onChange={(e) => updateColor("primaryLight", e.target.value)}
                        />
                      </div>
                    </div>
                    <div className="space-y-2">
                      <Label>{t("pages.whitelabel.primaryDark")}</Label>
                      <div className="flex items-center gap-2">
                        <Input
                          type="color"
                          className="h-10 w-12 p-1 cursor-pointer"
                          value={wl.primaryDark || "#4E93FF"}
                          onChange={(e) => updateColor("primaryDark", e.target.value)}
                        />
                        <Input
                          className="font-mono text-xs"
                          value={wl.primaryDark || "#4E93FF"}
                          onChange={(e) => updateColor("primaryDark", e.target.value)}
                        />
                      </div>
                    </div>
                  </div>
                </CardContent>
              </Card>

              {/* Cor dos Ícones do Menu */}
              <Card>
                <CardHeader>
                  <CardTitle>{t("pages.whitelabel.iconColorTitle", { defaultValue: "Cor dos Ícones do Menu" })}</CardTitle>
                  <CardDescription>
                    {t("pages.whitelabel.iconColorDesc", {
                      defaultValue: "Opcional. Se não definida, os ícones seguem a cor primária acima.",
                    })}
                  </CardDescription>
                </CardHeader>
                <CardContent className="space-y-4">
                  <div className="grid grid-cols-2 gap-4">
                    <div className="space-y-2">
                      <Label>{t("pages.whitelabel.iconColorLight", { defaultValue: "Ícones (Claro)" })}</Label>
                      <div className="flex items-center gap-2">
                        <Input
                          type="color"
                          className="h-10 w-12 p-1 cursor-pointer"
                          value={wl.iconColorLight || previewPrimary}
                          onChange={(e) => updateColor("iconColorLight", e.target.value)}
                        />
                        <Input
                          className="font-mono text-xs"
                          value={wl.iconColorLight || ""}
                          placeholder={t("pages.whitelabel.iconColorPlaceholder", { defaultValue: "usa a primária" })}
                          onChange={(e) => updateColor("iconColorLight", e.target.value)}
                        />
                      </div>
                    </div>
                    <div className="space-y-2">
                      <Label>{t("pages.whitelabel.iconColorDark", { defaultValue: "Ícones (Escuro)" })}</Label>
                      <div className="flex items-center gap-2">
                        <Input
                          type="color"
                          className="h-10 w-12 p-1 cursor-pointer"
                          value={wl.iconColorDark || previewPrimary}
                          onChange={(e) => updateColor("iconColorDark", e.target.value)}
                        />
                        <Input
                          className="font-mono text-xs"
                          value={wl.iconColorDark || ""}
                          placeholder={t("pages.whitelabel.iconColorPlaceholder", { defaultValue: "usa a primária" })}
                          onChange={(e) => updateColor("iconColorDark", e.target.value)}
                        />
                      </div>
                    </div>
                  </div>
                  {(wl.iconColorLight || wl.iconColorDark) && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="h-auto p-0 text-xs text-muted-foreground"
                      onClick={() => setWl((prev) => ({ ...prev, iconColorLight: "", iconColorDark: "" }))}
                    >
                      {t("pages.whitelabel.iconColorReset", { defaultValue: "Voltar a usar a cor primária" })}
                    </Button>
                  )}
                </CardContent>
              </Card>
            </div>

            {/* Logotipos e Splash */}
            <Card>
              <CardHeader>
                <CardTitle>{t("pages.whitelabel.assetsTitle")}</CardTitle>
                <CardDescription>{t("pages.whitelabel.assetsDesc")}</CardDescription>
              </CardHeader>
              <CardContent>
                <div className="grid gap-8 sm:grid-cols-2 lg:grid-cols-4">
                  <AssetUpload
                    label={t("pages.whitelabel.logoLight")}
                    value={wl.logoLight}
                    onUpload={(f) => handleUpload("logoLight", f)}
                    onRemove={() => removeAsset("logoLight")}
                    loading={uploadingKind === "logoLight"}
                  />
                  <AssetUpload
                    label={t("pages.whitelabel.logoDark")}
                    value={wl.logoDark}
                    onUpload={(f) => handleUpload("logoDark", f)}
                    onRemove={() => removeAsset("logoDark")}
                    loading={uploadingKind === "logoDark"}
                  />
                  <AssetUpload
                    label={t("pages.whitelabel.favicon")}
                    value={wl.favicon}
                    onUpload={(f) => handleUpload("favicon", f)}
                    onRemove={() => removeAsset("favicon")}
                    loading={uploadingKind === "favicon"}
                    square
                  />
                  <AssetUpload
                    label={t("pages.whitelabel.splash")}
                    value={wl.splash}
                    onUpload={(f) => handleUpload("splash", f)}
                    onRemove={() => removeAsset("splash")}
                    loading={uploadingKind === "splash"}
                    square
                  />
                </div>
              </CardContent>
            </Card>

            <div className="flex justify-end">
              <Button onClick={save} disabled={saving} size="lg" className="min-w-[150px]">
                {saving ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Save className="mr-2 h-4 w-4" />}
                {t("common.save")}
              </Button>
            </div>
          </div>

          {/* Live Preview Side Column */}
          <div className="space-y-6">
            <Card className="sticky top-6 overflow-hidden border-primary/20 bg-muted/20">
              <CardHeader className="bg-muted/50 pb-4">
                <div className="flex items-center gap-2">
                  <Eye className="h-4 w-4 text-primary" />
                  <CardTitle className="text-sm font-bold uppercase tracking-wider">
                    {t("pages.whitelabel.preview", { defaultValue: "Live Preview" })}
                  </CardTitle>
                </div>
                <CardDescription>
                  {t("pages.whitelabel.previewDesc", { defaultValue: "See how your brand will look in the system." })}
                </CardDescription>
              </CardHeader>
              <CardContent className="p-0">
                <div className="space-y-6 p-4">
                  {/* Browser Tab Preview */}
                  <div className="space-y-2">
                    <Label className="text-[10px] font-bold uppercase text-muted-foreground">{t("pages.whitelabel.browserTab", { defaultValue: "Browser Tab" })}</Label>
                    <div className="flex items-center gap-2 rounded-t-lg border bg-background px-3 py-1.5 shadow-sm">
                      <div className="h-4 w-4 shrink-0 overflow-hidden rounded-sm bg-muted flex items-center justify-center">
                        {wl.favicon ? (
                          <img src={wl.favicon} alt="Favicon" className="h-full w-full object-contain" />
                        ) : (
                          <Layout className="h-3 w-3 text-muted-foreground" />
                        )}
                      </div>
                      <span className="truncate text-xs font-medium text-foreground">{previewName}</span>
                    </div>
                  </div>

                  {/* Splash Screen Preview */}
                  <div className="space-y-2">
                    <Label className="text-[10px] font-bold uppercase text-muted-foreground">{t("pages.whitelabel.splashPreview", { defaultValue: "Splash Screen" })}</Label>
                    <div className="flex justify-center">
                      <div className="relative h-[200px] w-[110px] rounded-xl border-4 border-muted-foreground/20 bg-background shadow-lg overflow-hidden flex flex-col items-center justify-center gap-3">
                        <div className="h-10 w-10 rounded-full border border-border bg-card shadow-sm flex items-center justify-center overflow-hidden animate-pulse">
                          <img src={previewSplash} alt="Splash" className="h-7 w-7 object-contain" />
                        </div>
                        <div className="flex gap-0.5">
                          <span className="h-1 w-1 rounded-full bg-primary" />
                          <span className="h-1 w-1 rounded-full bg-primary opacity-50" />
                          <span className="h-1 w-1 rounded-full bg-primary opacity-25" />
                        </div>
                        <div className="absolute bottom-2 h-1 w-8 rounded-full bg-muted-foreground/20" />
                      </div>
                    </div>
                  </div>

                  {/* Sidebar Header Preview */}
                  <div className="space-y-2">
                    <Label className="text-[10px] font-bold uppercase text-muted-foreground">{t("pages.whitelabel.sidebarMenu", { defaultValue: "Side Menu" })}</Label>
                    <div className="rounded-lg border bg-background shadow-md overflow-hidden">
                      <div className="flex h-12 items-center border-b px-3">
                        <div className="flex flex-1 items-center justify-center overflow-hidden h-8">
                          {previewLogo ? (
                            <img src={previewLogo} alt="Logo" className="max-h-full max-w-full object-contain" />
                          ) : (
                            <div className="flex items-center gap-2">
                              <div
                                className="h-6 w-6 rounded flex items-center justify-center"
                                style={{ backgroundColor: previewPrimary, color: previewPrimaryText }}
                              >
                                <Monitor className="h-3 w-3" />
                              </div>
                              <span className="text-xs font-bold">{previewName}</span>
                            </div>
                          )}
                        </div>
                      </div>
                      <div className="p-3 space-y-2">
                        <div className="h-2 w-full rounded bg-muted/50" />
                        <div className="h-2 w-4/5 rounded bg-muted/50" />
                        <div className="h-8 w-full rounded-md mt-4" style={{ backgroundColor: `${previewIconColor}20` }}>
                          <div className="flex items-center gap-2 h-full px-2">
                            <div className="h-4 w-4 rounded" style={{ backgroundColor: previewIconColor }} />
                            <div className="h-2 w-12 rounded bg-foreground/10" />
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>

                  {/* Button & UI Preview */}
                  <div className="space-y-2">
                    <Label className="text-[10px] font-bold uppercase text-muted-foreground">{t("pages.whitelabel.interfacePreview", { defaultValue: "Interface" })}</Label>
                    <div className="rounded-lg border bg-background p-4 shadow-sm space-y-3">
                      <div className="flex items-center gap-3">
                        <div className="h-10 w-10 rounded-full bg-muted flex items-center justify-center overflow-hidden">
                          <Users2 className="h-5 w-5 text-muted-foreground" />
                        </div>
                        <div className="space-y-1">
                          <div className="h-2.5 w-24 rounded bg-muted" />
                          <div className="h-2 w-32 rounded bg-muted/40" />
                        </div>
                      </div>
                      <div className="flex gap-2 pt-1">
                        <div
                          className="flex-1 h-8 rounded-md flex items-center justify-center text-[10px] font-bold shadow-sm"
                          style={{ backgroundColor: previewPrimary, color: previewPrimaryText }}
                        >
                          {t("pages.whitelabel.previewButtonPrimary", { defaultValue: "PRIMARY BUTTON" })}
                        </div>
                        <div className="flex-1 h-8 rounded-md border flex items-center justify-center text-[10px] font-bold text-muted-foreground">
                          {t("pages.whitelabel.previewButtonCancel", { defaultValue: "CANCEL" })}
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>
        </div>
      </div>

      <ConfirmDialog
        open={restoring}
        onOpenChange={setRestoring}
        title={t("pages.whitelabel.restore")}
        description={t("pages.whitelabel.restoreConfirm")}
        onConfirm={handleRestore}
        destructive
      />
    </AppShell>
  );
};

const AssetUpload = ({ label, value, onUpload, onRemove, loading, square }: { 
  label: string; value?: string; onUpload: (f: File) => void; onRemove: () => void; loading?: boolean; square?: boolean;
}) => {
  const inputRef = useRef<HTMLInputElement>(null);
  const { t } = useTranslation();

  return (
    <div className="space-y-3">
      <Label>{label}</Label>
      <div 
        className={`relative flex items-center justify-center rounded-lg border-2 border-dashed bg-muted/30 p-4 transition-colors hover:bg-muted/50 ${
          square ? "aspect-square max-w-[120px]" : "h-32 w-full"
        }`}
      >
        {loading ? (
          <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        ) : value ? (
          <>
            <img src={value} alt={label} className="max-h-full max-w-full object-contain" />
            <Button
              variant="destructive"
              size="icon"
              className="absolute -right-2 -top-2 h-7 w-7 rounded-full shadow-lg"
              onClick={onRemove}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </>
        ) : (
          <div className="flex flex-col items-center gap-1 text-center">
            <Upload className="h-6 w-6 text-muted-foreground" />
            <Button variant="link" size="sm" onClick={() => inputRef.current?.click()} className="h-auto p-0">
              {t("pages.whitelabel.clickToUpload")}
            </Button>
          </div>
        )}
        <input
          type="file"
          ref={inputRef}
          className="hidden"
          accept="image/*"
          onChange={(e) => {
            const f = e.target.files?.[0];
            if (f) onUpload(f);
          }}
        />
      </div>
    </div>
  );
};

// Re-using icon from AppShell since it's used in preview
const Users2 = ({ className }: { className?: string }) => (
  <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className}><path d="M14 19a6 6 0 0 0-12 0"/><circle cx="8" cy="9" r="4"/><path d="M22 19a6 6 0 0 0-6-6 4 4 0 1 0 0-8"/></svg>
);

export default WhitelabelSettingsPage;
